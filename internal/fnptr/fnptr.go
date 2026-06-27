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
	Name  string
	Index int
	FnPtr bool
}

var (
	reTypedefFn    = regexp.MustCompile(`typedef\s+[\w\s\*]+\(\s*\*\s*(\w+)\s*\)\s*\(`)
	reStructHdr    = regexp.MustCompile(`\bstruct\s+(\w+)\s*\{`)
	reFieldFnPtr   = regexp.MustCompile(`\(\s*\*\s*(\w+)\s*\)\s*\(`) // RET (*name)(...)
	reInitHdr      = regexp.MustCompile(`\bstruct\s+(\w+)\s+\w+(?:\[\s*\w*\s*\])?\s*=\s*\{`)
	reDesignated   = regexp.MustCompile(`^\.\s*(\w+)\s*=\s*&?\s*(\w+)\s*$`)
	reIdent        = regexp.MustCompile(`^&?\s*(\w+)\s*$`)
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
	if len(keys) == 0 {
		return nil
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
			ln = stripComment(ln)
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
						return out
					}
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
	funcDefs       map[string]bool // function names with a definition (real-function gate)
}

func build(root string) *index {
	ix := &index{
		lines:          map[string][]string{},
		fnPtrTypedefs:  map[string]bool{},
		structLayout:   map[string][]fieldInfo{},
		fieldToStructs: map[string][]string{},
		registrations:  map[reg][]string{},
		funcDefs:       map[string]bool{},
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
	return ix
}

func (ix *index) scanStructs(f string) {
	lines := ix.lines[f]
	joined := strings.Join(lines, "\n")
	for _, loc := range reStructHdr.FindAllStringSubmatchIndex(joined, -1) {
		name := joined[loc[2]:loc[3]]
		body, ok := braceBody(joined, loc[1]-1)
		if !ok {
			continue
		}
		var fields []fieldInfo
		idx := 0
		for _, decl := range strings.Split(body, ";") {
			decl = strings.TrimSpace(stripComment(decl))
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
					}
				}
			}
			if fi.Name != "" {
				fields = append(fields, fi)
				idx++
			}
		}
		ix.structLayout[name] = fields
	}
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
	lines := ix.lines[f]
	joined := strings.Join(lines, "\n")
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
		// items may themselves be braces (tables); flatten top-level commas
		pos := 0
		for _, item := range splitTopLevel(body) {
			item = strings.TrimSpace(stripComment(item))
			if item == "" {
				continue
			}
			// nested braces -> a table row: recurse one level positionally
			if strings.HasPrefix(item, "{") {
				ix.scanRow(st, layout, strings.Trim(item, "{} "))
				continue
			}
			if m := reDesignated.FindStringSubmatch(item); m != nil {
				if ix.fnPtrField(st, m[1]) {
					ix.addReg(st, m[1], m[2])
				}
				continue
			}
			// positional at top level
			if pos < len(layout) && layout[pos].FnPtr {
				if id := reIdent.FindStringSubmatch(item); id != nil {
					ix.addReg(st, layout[pos].Name, id[1])
				}
			}
			pos++
		}
	}
}

// scanRow handles one positional/designated row of a table initializer.
func (ix *index) scanRow(st string, layout []fieldInfo, row string) {
	pos := 0
	for _, item := range splitTopLevel(row) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if m := reDesignated.FindStringSubmatch(item); m != nil {
			if ix.fnPtrField(st, m[1]) {
				ix.addReg(st, m[1], m[2])
			}
			continue
		}
		if pos < len(layout) && layout[pos].FnPtr {
			if id := reIdent.FindStringSubmatch(item); id != nil {
				ix.addReg(st, layout[pos].Name, id[1])
			}
		}
		pos++
	}
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
			out = append(out, p)
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
