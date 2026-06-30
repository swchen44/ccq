// Package cindex is a pure-text, preprocessor-blind C/C++ definition index. It
// scans every source file and records where functions, structs/unions/enums,
// typedefs and macros are DEFINED — without evaluating `#ifdef`, so it sees
// definitions inside disabled config branches that clangd drops in no-build
// mode. It is a deliberate, clearly-labelled fallback for `ccq def` when clangd
// finds nothing; it never feeds the precise (clangd) path.
package cindex

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/swchen44/ccq/internal/csrc"
)

// Def is one definition site of a symbol.
type Def struct {
	File string
	Line int    // 1-based
	Kind string // func | struct | union | enum | typedef | define
}

// Index maps a symbol name to its definition sites.
type Index struct {
	defs map[string][]Def
}

// Lookup returns the recorded definitions of name (nil if none).
func (ix *Index) Lookup(name string) []Def { return ix.defs[name] }

var (
	reFuncBrace = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\([^()]*\)\s*\{`) // RET name(args) {
	reFuncHdr   = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\([^;{}]*\)\s*$`) // RET name(args)   (brace on next line)
	reAggregate = regexp.MustCompile(`\b(struct|union|enum)\s+(\w+)\s*\{`) // struct|union|enum TAG {
	reTypedefNm = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*;\s*$`)           // trailing `... NAME;`
	reTdClose   = regexp.MustCompile(`^\s*\}\s*([A-Za-z_]\w*)\s*;`)        // `} NAME;` closing a typedef'd aggregate
	reDefine    = regexp.MustCompile(`^\s*#\s*define\s+([A-Za-z_]\w*)`)    // #define NAME
)

// keywords that reFuncBrace/reFuncHdr could otherwise mistake for a function
// name (control-flow / operators that are followed by `(...) {`).
var keywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "return": true,
	"sizeof": true, "do": true, "else": true, "struct": true, "union": true,
	"enum": true, "typedef": true, "case": true, "default": true, "goto": true,
}

var (
	cacheMu   sync.Mutex
	cacheRoot string
	cacheIdx  *Index
)

// Invalidate drops the cached index.
func Invalidate() {
	cacheMu.Lock()
	cacheRoot, cacheIdx = "", nil
	cacheMu.Unlock()
}

// Build returns the definition index for root, cached for the process lifetime.
func Build(root string) *Index {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheIdx != nil && cacheRoot == root {
		return cacheIdx
	}
	ix := buildFresh(root)
	cacheRoot, cacheIdx = root, ix
	return ix
}

func buildFresh(root string) *Index {
	ix := &Index{defs: map[string][]Def{}}
	seen := map[string]bool{} // name|file|line dedup
	add := func(name, file string, line int, kind string) {
		if name == "" || keywords[name] {
			return
		}
		key := name + "|" + file + "|" + strconv.Itoa(line)
		if seen[key] {
			return
		}
		seen[key] = true
		ix.defs[name] = append(ix.defs[name], Def{File: file, Line: line, Kind: kind})
	}

	for _, f := range csrc.Files(root) {
		raw := csrc.ReadLines(f)
		// Strip comments across the whole file (multi-line aware, newline-preserving),
		// then neutralize string/char literals per line. The result never evaluates
		// the preprocessor, so code inside any #ifdef branch stays visible.
		joined := csrc.StripComments(strings.Join(raw, "\n"))
		lines := strings.Split(joined, "\n")
		for i := range lines {
			lines[i] = csrc.StripCodeLine(lines[i])
		}

		depth := 0
		pendingTypedef := false
		for i, s := range lines {
			trimmed := strings.TrimSpace(s)
			startDepth := depth
			depth += strings.Count(s, "{") - strings.Count(s, "}")

			// #define anywhere.
			if m := reDefine.FindStringSubmatch(s); m != nil {
				add(m[1], f, i+1, "define")
			}

			// typedef: single-line completes with `;`; otherwise it opens a
			// multi-line aggregate closed later by `} NAME;`.
			if strings.HasPrefix(trimmed, "typedef") {
				if strings.HasSuffix(trimmed, ";") {
					if m := reTypedefNm.FindStringSubmatch(trimmed); m != nil {
						add(m[1], f, i+1, "typedef")
					}
				} else {
					pendingTypedef = true
				}
			} else if pendingTypedef {
				if m := reTdClose.FindStringSubmatch(s); m != nil {
					add(m[1], f, i+1, "typedef")
					pendingTypedef = false
				}
			}

			// struct/union/enum TAG { ... } — the named aggregate definition.
			for _, m := range reAggregate.FindAllStringSubmatch(s, -1) {
				add(m[2], f, i+1, m[1])
			}

			// File-scope function definitions only (startDepth 0): same-line brace,
			// or a header line whose brace is on the next non-empty line.
			if startDepth == 0 {
				if m := reFuncBrace.FindStringSubmatch(s); m != nil {
					add(m[1], f, i+1, "func")
				} else if m := reFuncHdr.FindStringSubmatch(s); m != nil {
					if nextNonEmptyStartsBrace(lines, i+1) {
						add(m[1], f, i+1, "func")
					}
				}
			}
		}
	}
	return ix
}

// nextNonEmptyStartsBrace reports whether the first non-empty line at/after idx
// begins with `{` (a function body opened on the line after its header).
func nextNonEmptyStartsBrace(lines []string, idx int) bool {
	for j := idx; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		return strings.HasPrefix(t, "{")
	}
	return false
}
