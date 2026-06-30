// Package fnptr adds function-pointer dispatch resolution on top of clangd.
// clangd (correctly) does not guess which handler a runtime `obj->fn()` call
// reaches; it only links the static registration `.fn = handler`. This package
// synthesizes the dispatcher->handler edge that CodeGraph's heuristic produces:
//
//	registration: struct ops X = { .scan = wext_scan };  (or positional {"n", wext_scan})
//	dispatch:     drv->scan(...);  -> enclosing function is a heuristic caller of wext_scan
//
// Keyed by (struct type, field) so distinct structs that happen to share a
// fn-pointer field name do not cross-bleed. Over-approximation is bounded by
// FANOUT_CAP and a real-function gate. Ported from CodeGraph's
// c-fnptr-synthesizer.ts algorithm.
package fnptr

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/swchen44/ccq/internal/config"
)

// Caller is a synthesized heuristic caller of a target handler.
type Caller struct {
	Func  string // enclosing function at the dispatch site
	File  string
	Line  int
	Field string // "struct.field" that bridged dispatcher->handler
}

type reg struct{ Struct, Field string }

type fieldInfo struct {
	Name       string
	Index      int
	FnPtr      bool
	StructType string // for a value `struct/union TAG name;` field: the inner tag
}

var (
	reTypedefFn    = regexp.MustCompile(`typedef\s+[\w\s\*]+\(\s*\*\s*(\w+)\s*\)\s*\(`)
	reStructAny    = regexp.MustCompile(`(typedef\s+)?(?:struct|union)\s+(\w*)\s*\{`)                        // [typedef] struct|union [TAG] {
	reFieldFnPtr   = regexp.MustCompile(`\(\s*\*\s*(\w+)\s*\)\s*\(`)                                         // RET (*name)(...)
	reInitHdr      = regexp.MustCompile(`(?:(?:struct|union)\s+)?(\w+)\s+\w+\s*(?:\[\s*\w*\s*\])?\s*=\s*\{`) // [struct|union] TYPE name[] = {
	reDesignated   = regexp.MustCompile(`^\.\s*(\w+)\s*=\s*(.+)$`)                                           // .field = <value>
	reArrayIdx     = regexp.MustCompile(`^\[\s*[^\]]*\]\s*=\s*(.+)$`)                                        // [N] = <value>
	reIdent        = regexp.MustCompile(`^&?\s*(\w+)\s*$`)
	reCast         = regexp.MustCompile(`^\(\s*[\w\s\*]+\)\s*(.+)$`) // (type) expr  -> expr
	reMacro1       = regexp.MustCompile(`^(\w+)\s*\(\s*(.+?)\s*\)$`) // MACRO(inner)
	reDispatch     = regexp.MustCompile(`(\w+)\s*(?:->|\.)\s*(\w+)\s*\(`)
	reFieldAssign  = regexp.MustCompile(`(\w+)\s*(?:->|\.)\s*(\w+)\s*=\s*(\w+)\s*(?:->|\.)\s*(\w+)`)
	reFuncDefBrace = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\([^()]*\)\s*\{`)
	reFuncDefHdr   = regexp.MustCompile(`^[A-Za-z_].*\b([A-Za-z_]\w*)\s*\([^;{]*$`)
)

const fanoutCap = 300

// Callers returns heuristic callers of handler, keyed by (struct, field).
func Callers(root, handler string) []Caller {
	idx := build(root)
	// which (struct,field) keys is handler registered to?
	var keys []reg
	for k, hs := range idx.registrations {
		for _, h := range hs {
			if h == handler {
				keys = append(keys, k)
				break
			}
		}
	}
	manual := idx.manualLinks[handler]
	if len(keys) == 0 {
		return manual // may be nil; or direct edges from the override table
	}
	keySet := map[reg]bool{}
	for _, k := range keys {
		keySet[k] = true
	}
	// dispatch sites whose (resolved struct, field) is one of handler's keys
	var out []Caller
	seen := map[string]bool{}
	added := 0
	for _, f := range idx.files {
		lines := idx.lines[f]
		for i, ln := range lines {
			ln = stripCodeLine(ln)
			for _, m := range reDispatch.FindAllStringSubmatch(ln, -1) {
				recv, field := m[1], m[2]
				owners := idx.fieldToStructs[field]
				if len(owners) == 0 {
					continue
				}
				st := idx.recvType(lines, i, recv)
				if st == "" || !contains(owners, st) {
					if len(owners) == 1 {
						st = owners[0]
					} else {
						continue
					}
				}
				if !keySet[reg{st, field}] {
					continue
				}
				fn := enclosingFunc(lines, i)
				key := fn + "|" + st + "." + field
				if fn != "" && fn != handler && !seen[key] {
					seen[key] = true
					out = append(out, Caller{Func: fn, File: f, Line: i + 1, Field: st + "." + field})
					if added++; added >= fanoutCap {
						return append(out, manual...)
					}
				}
			}
		}
	}
	return append(out, manual...)
}

// Callees returns the handlers that fn dispatches to via `obj->field()` (the
// reverse of Callers): for each dispatch site inside fn it resolves the
// (struct, field) and returns the registered handlers, plus any manual links
// declared from fn. Used to enrich `ccq callees`.
func Callees(root, fn string) []string {
	idx := build(root)
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		if h != "" && h != fn && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	// manual links declared from fn
	for h, callers := range idx.manualLinks {
		for _, c := range callers {
			if c.Func == fn {
				add(h)
			}
		}
	}
	// dispatch sites whose enclosing function is fn
	for _, f := range idx.files {
		lines := idx.lines[f]
		for i, ln := range lines {
			s := stripCodeLine(ln)
			if !reDispatch.MatchString(s) || enclosingFunc(lines, i) != fn {
				continue
			}
			for _, m := range reDispatch.FindAllStringSubmatch(s, -1) {
				recv, field := m[1], m[2]
				owners := idx.fieldToStructs[field]
				if len(owners) == 0 {
					continue
				}
				st := idx.recvType(lines, i, recv)
				if st == "" || !contains(owners, st) {
					if len(owners) == 1 {
						st = owners[0]
					} else {
						continue
					}
				}
				for _, h := range idx.registrations[reg{st, field}] {
					add(h)
				}
			}
		}
	}
	return out
}

type index struct {
	files          []string
	lines          map[string][]string
	fnPtrTypedefs  map[string]bool
	structLayout   map[string][]fieldInfo
	fieldToStructs map[string][]string
	registrations  map[reg][]string
	funcDefs       map[string]bool     // function names with a definition (real-function gate)
	manualLinks    map[string][]Caller // handler -> direct callers from the override table
}

var (
	cacheMu   sync.Mutex
	cacheRoot string
	cacheIdx  *index
)

// Invalidate drops the cached index (call after files change within a long-lived
// daemon session if you want fn-pointer results to reflect edits immediately).
func Invalidate() {
	cacheMu.Lock()
	cacheRoot, cacheIdx = "", nil
	cacheMu.Unlock()
}

// build returns the fn-pointer index for root, cached for the process lifetime so
// repeated queries on a warm daemon don't rescan the whole repo each time.
func build(root string) *index {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheIdx != nil && cacheRoot == root {
		return cacheIdx
	}
	ix := buildFresh(root)
	cacheRoot, cacheIdx = root, ix
	return ix
}

func buildFresh(root string) *index {
	ix := &index{
		lines:          map[string][]string{},
		fnPtrTypedefs:  map[string]bool{},
		structLayout:   map[string][]fieldInfo{},
		fieldToStructs: map[string][]string{},
		registrations:  map[reg][]string{},
		funcDefs:       map[string]bool{},
		manualLinks:    map[string][]Caller{},
	}
	ix.files = cFiles(root)
	for _, f := range ix.files {
		ix.lines[f] = readLines(f)
	}
	// Pass A: fn-pointer typedefs + function defs (real-function gate)
	for _, f := range ix.files {
		joined := strings.Join(ix.lines[f], "\n")
		for _, m := range reTypedefFn.FindAllStringSubmatch(joined, -1) {
			ix.fnPtrTypedefs[m[1]] = true
		}
		for _, ln := range ix.lines[f] {
			s := stripComment(ln)
			for _, m := range reFuncDefBrace.FindAllStringSubmatch(s, -1) {
				if !isKeyword(m[1]) {
					ix.funcDefs[m[1]] = true
				}
			}
			if m := reFuncDefHdr.FindStringSubmatch(strings.TrimSpace(s)); m != nil && !isKeyword(m[1]) {
				ix.funcDefs[m[1]] = true
			}
		}
	}
	// Pass B: struct layouts
	for _, f := range ix.files {
		ix.scanStructs(f)
	}
	for st, fields := range ix.structLayout {
		for _, fi := range fields {
			if fi.FnPtr {
				ix.fieldToStructs[fi.Name] = appendUniq(ix.fieldToStructs[fi.Name], st)
			}
		}
	}
	// Pass C: registrations (designated + positional)
	for _, f := range ix.files {
		ix.scanRegistrations(f)
	}
	// Pass D: field<-field propagation (3 passes to converge)
	props := ix.scanPropagations()
	for pass := 0; pass < 3; pass++ {
		changed := false
		for _, p := range props {
			from := ix.registrations[p.from]
			for _, h := range from {
				if !contains(ix.registrations[p.to], h) {
					ix.registrations[p.to] = append(ix.registrations[p.to], h)
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	// Manual override table (ground truth the text scan can't infer).
	if t, _, err := LoadTable(root); err == nil && t != nil {
		ix.mergeTable(t)
	}
	return ix
}

// mergeTable folds a user override into the index: registrations augment the
// auto-discovered (struct,field) map (bypassing the real-function gate, since
// the user declared them); links become direct dispatcher→handler edges.
func (ix *index) mergeTable(t *Table) {
	for _, r := range t.Registrations {
		// ensure the field is known as a fn-pointer field so dispatch detection links it
		if !structHasField(ix.structLayout[r.Struct], r.Field) {
			ix.structLayout[r.Struct] = append(ix.structLayout[r.Struct],
				fieldInfo{Name: r.Field, Index: len(ix.structLayout[r.Struct]), FnPtr: true})
		}
		ix.fieldToStructs[r.Field] = appendUniq(ix.fieldToStructs[r.Field], r.Struct)
		k := reg{r.Struct, r.Field}
		for _, h := range r.Handlers {
			if !contains(ix.registrations[k], h) {
				ix.registrations[k] = append(ix.registrations[k], h)
			}
		}
	}
	for _, l := range t.Links {
		field := "manual"
		if l.Note != "" {
			field = "manual:" + l.Note
		}
		for _, h := range l.To {
			ix.manualLinks[h] = append(ix.manualLinks[h], Caller{Func: l.From, File: "ccq.fnptr.json", Line: 0, Field: field})
		}
	}
}

func (ix *index) scanStructs(f string) {
	joined := stripComments(strings.Join(ix.lines[f], "\n"))
	for _, loc := range reStructAny.FindAllStringSubmatchIndex(joined, -1) {
		isTypedef := loc[2] >= 0 // group 1 (typedef) present
		tag := joined[loc[4]:loc[5]]
		body, end, ok := braceBodyEnd(joined, loc[1]-1)
		if !ok {
			continue
		}
		fields := ix.parseStructFields(body)
		if tag != "" {
			ix.structLayout[tag] = fields
		}
		// `typedef struct [TAG] { ... } ALIAS;` — also key the layout by ALIAS
		if isTypedef {
			if alias := firstIdentAfter(joined, end); alias != "" {
				ix.structLayout[alias] = fields
			}
		}
	}
}

// parseStructFields turns a struct body into ordered fields, marking fn-pointer
// fields (inline `RET (*name)(...)` or a typedef'd fn-pointer type).
func (ix *index) parseStructFields(body string) []fieldInfo {
	var fields []fieldInfo
	idx := 0
	for _, decl := range strings.Split(body, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		fi := fieldInfo{Index: idx}
		if m := reFieldFnPtr.FindStringSubmatch(decl); m != nil {
			fi.Name = m[1]
			fi.FnPtr = true
		} else {
			// typedef'd fn-pointer field:  HOOK_T name;
			toks := strings.Fields(decl)
			if len(toks) >= 2 {
				fi.Name = strings.Trim(toks[len(toks)-1], "*[]")
				if ix.fnPtrTypedefs[toks[0]] {
					fi.FnPtr = true
				} else if (toks[0] == "struct" || toks[0] == "union") && len(toks) >= 3 && !strings.Contains(decl, "*") {
					// value `struct/union TAG name;` — field holds an inner aggregate
					fi.StructType = toks[1]
				}
			}
		}
		if fi.Name != "" {
			fields = append(fields, fi)
			idx++
		}
	}
	return fields
}

// fieldStructType returns the inner struct/union tag held by a value-typed field,
// or "" if the field is not an aggregate value field.
func (ix *index) fieldStructType(st, field string) string {
	for _, fi := range ix.structLayout[st] {
		if fi.Name == field {
			return fi.StructType
		}
	}
	return ""
}

func (ix *index) fnPtrField(st, field string) bool {
	for _, fi := range ix.structLayout[st] {
		if fi.Name == field {
			return fi.FnPtr
		}
	}
	return false
}

func (ix *index) addReg(st, field, fn string) {
	if !ix.funcDefs[fn] { // real-function gate
		return
	}
	k := reg{st, field}
	if !contains(ix.registrations[k], fn) {
		ix.registrations[k] = append(ix.registrations[k], fn)
	}
}

func (ix *index) scanRegistrations(f string) {
	joined := stripComments(strings.Join(ix.lines[f], "\n"))
	for _, loc := range reInitHdr.FindAllStringSubmatchIndex(joined, -1) {
		st := joined[loc[2]:loc[3]]
		layout := ix.structLayout[st]
		if layout == nil {
			continue
		}
		body, ok := braceBody(joined, loc[1]-1)
		if !ok {
			continue
		}
		ix.scanRow(st, layout, body) // top level is just a (possibly nested) row
	}
}

// scanRow handles one positional/designated row of a struct/table initializer.
// Items may be designated (`.f = h`), positional, or a nested brace (a table row
// or a brace-wrapped scalar for the current field). pos tracks the positional
// cursor; a designated entry moves it past that field's index (C semantics).
func (ix *index) scanRow(st string, layout []fieldInfo, row string) {
	pos := 0
	for _, item := range splitTopLevel(row) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		// array-index designator `[N] = <value>`: the index selects an array
		// element, so the value (typically a `{...}` struct row) is handled by
		// the logic below as if it were positional at the current cursor.
		if m := reArrayIdx.FindStringSubmatch(item); m != nil {
			item = strings.TrimSpace(m[1])
		}
		if strings.HasPrefix(item, "{") {
			inner := strings.Trim(item, "{} ")
			if strings.ContainsAny(inner, ",{") { // genuine nested row
				ix.scanRow(st, layout, inner)
			} else { // brace-wrapped scalar for the current field
				ix.placePositional(st, layout, pos, inner)
			}
			pos++
			continue
		}
		if m := reDesignated.FindStringSubmatch(item); m != nil {
			field, val := m[1], strings.TrimSpace(m[2])
			if ix.fnPtrField(st, field) {
				if h := handlerIdent(val); h != "" {
					ix.addReg(st, field, h)
				}
			} else if inner := ix.fieldStructType(st, field); inner != "" && strings.HasPrefix(val, "{") {
				// nested struct init `.f = { ... }`: recurse with the inner layout
				if il := ix.structLayout[inner]; il != nil {
					ix.scanRow(inner, il, strings.Trim(val, "{} "))
				}
			}
			pos = ix.fieldIndex(st, field) + 1 // positional continues after this field
			continue
		}
		ix.placePositional(st, layout, pos, item)
		pos++
	}
}

// placePositional registers item to layout[pos] if that field is a fn-pointer.
func (ix *index) placePositional(st string, layout []fieldInfo, pos int, item string) {
	if pos >= 0 && pos < len(layout) && layout[pos].FnPtr {
		if h := handlerIdent(item); h != "" {
			ix.addReg(st, layout[pos].Name, h)
		}
	}
}

func (ix *index) fieldIndex(st, field string) int {
	for _, fi := range ix.structLayout[st] {
		if fi.Name == field {
			return fi.Index
		}
	}
	return -1
}

// handlerIdent extracts a function identifier from a registration value, peeling
// an optional leading cast `(type)` and one-arg macro `WRAP(fn)`. Returns "" if
// it isn't a bare identifier (the real-function gate in addReg filters the rest).
func handlerIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
	if m := reCast.FindStringSubmatch(s); m != nil { // (type) X -> X
		s = strings.TrimSpace(m[1])
	}
	if m := reIdent.FindStringSubmatch(s); m != nil { // bare ident
		return m[1]
	}
	if m := reMacro1.FindStringSubmatch(s); m != nil { // MACRO(inner)
		if mm := reIdent.FindStringSubmatch(strings.TrimSpace(m[2])); mm != nil {
			return mm[1]
		}
	}
	return ""
}

type prop struct{ to, from reg }

func (ix *index) scanPropagations() []prop {
	var out []prop
	for _, f := range ix.files {
		lines := ix.lines[f]
		for i, ln := range lines {
			ln = stripComment(ln)
			for _, m := range reFieldAssign.FindAllStringSubmatch(ln, -1) {
				lrecv, lf, rrecv, rf := m[1], m[2], m[3], m[4]
				lt := ix.recvType(lines, i, lrecv)
				rt := ix.recvType(lines, i, rrecv)
				if lt != "" && rt != "" && ix.fnPtrField(lt, lf) && ix.fnPtrField(rt, rf) {
					out = append(out, prop{reg{lt, lf}, reg{rt, rf}})
				}
			}
		}
	}
	return out
}

// recvType infers the struct type of recv from the enclosing function's
// params/locals, returning only structs known to have fn-pointer fields.
func (ix *index) recvType(lines []string, atLine int, recv string) string {
	re := regexp.MustCompile(`(?:struct\s+)?(\w+)\s*\*?\s*\b` + regexp.QuoteMeta(recv) + `\b\s*(?:[,)=;]|\[)`)
	for i := atLine; i >= 0 && i > atLine-400; i-- {
		for _, m := range re.FindAllStringSubmatch(lines[i], -1) {
			if _, ok := ix.structLayout[m[1]]; ok {
				return m[1]
			}
		}
	}
	return ""
}

func enclosingFunc(lines []string, idx int) string {
	for i := idx; i >= 0 && i > idx-400; i-- {
		ln := stripComment(lines[i])
		for _, m := range reFuncDefBrace.FindAllStringSubmatch(ln, -1) {
			if !isKeyword(m[1]) {
				return m[1]
			}
		}
		if m := reFuncDefHdr.FindStringSubmatch(strings.TrimSpace(ln)); m != nil && !isKeyword(m[1]) {
			return m[1]
		}
	}
	return ""
}

// ---- small helpers ----

func stripComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	// crude /* */ on a single line
	for {
		a := strings.Index(s, "/*")
		if a < 0 {
			break
		}
		b := strings.Index(s[a:], "*/")
		if b < 0 {
			s = s[:a]
			break
		}
		s = s[:a] + s[a+b+2:]
	}
	return s
}

// stripCodeLine removes // and /* */ comments AND blanks the interior of string
// and char literals on a single line, so a dispatch-like token inside a string
// (e.g. "... p->fn() ...") is never parsed as a real call site. Used by the
// dispatch scan in Callers/Callees (where false positives must be avoided).
func stripCodeLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			break // line comment: drop the rest
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' { // block comment (single line)
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 3
				continue
			}
			break
		}
		if c == '"' || c == '\'' { // keep the delimiters, blank the interior
			b.WriteByte(c)
			i++
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == c {
					b.WriteByte(c)
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// stripComments removes // and (multi-line) /* */ comments from a whole block,
// preserving newlines so line numbers stay aligned. Used before the join-based
// struct/registration scans so comment commas/braces can't corrupt splitting.
func stripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			j := strings.Index(s[i+2:], "*/")
			seg := s[i:]
			if j >= 0 {
				seg = s[i : i+2+j+2]
			}
			for _, r := range seg { // keep the comment's newlines
				if r == '\n' {
					b.WriteByte('\n')
				}
			}
			if j < 0 {
				break
			}
			i += 2 + j + 2
			continue
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// firstIdentAfter returns the first identifier at/after pos (skipping `*` and
// spaces) — used to read the ALIAS in `typedef struct {...} ALIAS;`.
func firstIdentAfter(src string, pos int) string {
	i := pos
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '*' || src[i] == '\r') {
		i++
	}
	j := i
	for j < len(src) && (isWordByte(src[j])) {
		j++
	}
	if j > i {
		return src[i:j]
	}
	return ""
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// braceBodyEnd is braceBody plus the index just past the matching `}`.
func braceBodyEnd(src string, open int) (body string, end int, ok bool) {
	for open < len(src) && src[open] != '{' {
		open++
	}
	if open >= len(src) {
		return "", 0, false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// braceBody returns the text inside the braces that open at/after `open`.
func braceBody(src string, open int) (string, bool) {
	for open < len(src) && src[open] != '{' {
		open++
	}
	if open >= len(src) {
		return "", false
	}
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}

// splitTopLevel splits a brace/paren-balanced comma list at depth 0.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	last := 0
	for i, r := range s {
		switch r {
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[last:i])
				last = i + 1
			}
		}
	}
	out = append(out, s[last:])
	return out
}

func isKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "return", "sizeof", "do", "struct", "union", "enum":
		return true
	}
	return false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func appendUniq(ss []string, s string) []string {
	if contains(ss, s) {
		return ss
	}
	return append(ss, s)
}

func cFiles(root string) []string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				b := filepath.Base(p)
				if b == ".git" || b == "build" || b == "node_modules" || b == ".cache" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp":
			if config.Keep(p) {
				out = append(out, p)
			}
		}
		return nil
	})
	return out
}

func readLines(f string) []string {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}
