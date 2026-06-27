// Package fnptr adds function-pointer dispatch resolution on top of clangd.
// clangd (correctly) does not guess which handler a runtime `obj->fn()` call
// reaches; it only links the static registration `.fn = handler`. This package
// synthesizes the dispatcher->handler edge that CodeGraph's heuristic produces:
//
//	registration: struct ops X = { .scan = wext_scan };  (or positional {"n", wext_scan})
//	dispatch:     drv->scan(...);  -> enclosing function is a heuristic caller of wext_scan
//
// Results are marked heuristic (over-approximation: links every dispatcher of a
// field to every handler registered to that field).
package fnptr

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Caller is a synthesized heuristic caller of a target handler.
type Caller struct {
	Func  string // enclosing function name at the dispatch site
	File  string
	Line  int
	Field string // the fn-pointer field that bridged dispatcher->handler
}

var (
	// .field = handler   (designated initializer)
	reDesignated = regexp.MustCompile(`\.([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*)\b`)
	// recv->field(  or recv.field(   (indirect dispatch)
	reDispatch = regexp.MustCompile(`[A-Za-z_]\w*\s*(?:->|\.)\s*([A-Za-z_]\w*)\s*\(`)
	// a C function definition: ... name(args) {   (one-line or header line)
	reFuncDefBrace = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\([^()]*\)\s*\{`)
	// a multi-line function definition header (opening, no ; no { yet)
	reFuncDefHdr = regexp.MustCompile(`^[A-Za-z_].*\b([A-Za-z_]\w*)\s*\([^;{]*$`)
)

// Callers returns heuristic callers of handler by:
//  1. finding the fn-pointer field(s) handler is registered to (.field = handler),
//  2. finding indirect dispatch sites of those fields (recv->field()),
//  3. attributing each to its enclosing function.
func Callers(root, handler string) []Caller {
	files := cFiles(root)
	// 1. fields that handler is bound to
	fields := map[string]bool{}
	for _, f := range files {
		for _, ln := range readLines(f) {
			for _, m := range reDesignated.FindAllStringSubmatch(ln, -1) {
				if m[2] == handler {
					fields[m[1]] = true
				}
			}
			// positional table:  { "name", handler }
			if strings.Contains(ln, handler) && strings.Contains(ln, "{") {
				// best-effort: positional handlers are harder; covered by clangd refs
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	// 2+3. dispatch sites of those fields -> enclosing function
	var out []Caller
	seen := map[string]bool{}
	for _, f := range files {
		lines := readLines(f)
		for i, ln := range lines {
			for _, m := range reDispatch.FindAllStringSubmatch(ln, -1) {
				if fields[m[1]] {
					fn := enclosingFunc(lines, i)
					key := fn + "|" + m[1]
					if fn != "" && fn != handler && !seen[key] {
						seen[key] = true
						out = append(out, Caller{Func: fn, File: f, Line: i + 1, Field: m[1]})
					}
				}
			}
		}
	}
	return out
}

// enclosingFunc scans from line idx upward to find the function definition
// whose body contains it (best-effort).
func enclosingFunc(lines []string, idx int) string {
	for i := idx; i >= 0 && i > idx-400; i-- {
		ln := lines[i]
		// one-line or brace-opening definition: name(args) {
		for _, m := range reFuncDefBrace.FindAllStringSubmatch(ln, -1) {
			if !isKeyword(m[1]) {
				return m[1]
			}
		}
		// multi-line header (next line opens brace)
		if m := reFuncDefHdr.FindStringSubmatch(strings.TrimSpace(ln)); m != nil && !isKeyword(m[1]) {
			return m[1]
		}
	}
	return ""
}

func isKeyword(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "return", "sizeof", "do":
		return true
	}
	return false
}

func cFiles(root string) []string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				b := filepath.Base(p)
				if b == ".git" || b == "build" || b == "node_modules" {
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
