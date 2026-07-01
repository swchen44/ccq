// Package tsindex is an OPT-IN, tree-sitter (gotreesitter) backend for the
// pure-text definition index — an alternative to internal/cindex's regex
// heuristic, selected at runtime by `--treesitter` / CCQ_TREESITTER.
//
// It is #ifdef-blind (tree-sitter never runs the preprocessor) like cindex, and
// more robust on odd declaration forms (e.g. K&R column-0 names). BUT it is
// experimental and NOT the default: a single iterator/control-flow macro
// (`nla_for_each_nested(...) { ... };`) makes tree-sitter mis-parse and cascade
// through error-recovery, dropping every definition after it in the file — see
// docs/tree-sitter-exploration.md. Because gotreesitter's C grammar lives behind
// build tags (`grammar_subset grammar_subset_c`), a binary built WITHOUT those
// tags has no C grammar; Lookup then degrades gracefully to nil + a one-time
// notice, so `go build`/`go test` without the tags still work.
package tsindex

import (
	"fmt"
	"os"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/swchen44/ccq/internal/cindex"
	"github.com/swchen44/ccq/internal/csrc"
)

var (
	cacheMu   sync.Mutex
	cacheRoot string
	cacheDefs map[string][]cindex.Def
	noticed   bool
)

// Invalidate drops the cached index.
func Invalidate() {
	cacheMu.Lock()
	cacheRoot, cacheDefs = "", nil
	cacheMu.Unlock()
}

// Lookup returns the tree-sitter-derived definitions of name under root (cached
// per process). Returns nil if the C grammar was not compiled in.
func Lookup(root, name string) []cindex.Def {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheDefs == nil || cacheRoot != root {
		cacheDefs = buildFresh(root)
		cacheRoot = root
	}
	return cacheDefs[name]
}

func buildFresh(root string) map[string][]cindex.Def {
	defs := map[string][]cindex.Def{}
	entry := grammars.DetectLanguageByName("c")
	if entry == nil || entry.Language == nil {
		if !noticed {
			fmt.Fprintln(os.Stderr, "note: --treesitter has no effect — this binary was built without the tree-sitter C grammar (release builds use -tags 'grammar_subset grammar_subset_c')")
			noticed = true
		}
		return defs
	}
	lang := entry.Language()
	seen := map[string]bool{}
	add := func(name, file string, line int, kind string) {
		if name == "" {
			return
		}
		key := name + "|" + file + "|" + itoa(line)
		if seen[key] {
			return
		}
		seen[key] = true
		defs[name] = append(defs[name], cindex.Def{File: file, Line: line, Kind: kind})
	}

	for _, f := range csrc.Files(root) {
		src := readFile(f)
		if src == nil {
			continue
		}
		p := ts.NewParser(lang)
		tree, err := p.Parse(src)
		if err != nil || tree == nil {
			continue
		}
		ts.Walk(tree.RootNode(), func(n *ts.Node, depth int) ts.WalkAction {
			switch n.Type(lang) {
			case "function_definition":
				if nm := declName(n, lang, src); nm != "" {
					add(nm, f, int(n.StartPoint().Row)+1, "func")
				}
			case "struct_specifier", "union_specifier", "enum_specifier":
				if !hasBody(n, lang) { // a type USE (`struct foo *x`), not a definition
					return ts.WalkContinue
				}
				kind := map[string]string{"struct_specifier": "struct", "union_specifier": "union", "enum_specifier": "enum"}[n.Type(lang)]
				if nm := fieldText(n, "name", lang, src); nm != "" {
					add(nm, f, int(n.StartPoint().Row)+1, kind)
				}
			case "type_definition":
				if nm := declName(n, lang, src); nm != "" {
					add(nm, f, int(n.StartPoint().Row)+1, "typedef")
				}
			case "preproc_def", "preproc_function_def":
				if nm := fieldText(n, "name", lang, src); nm != "" {
					add(nm, f, int(n.StartPoint().Row)+1, "define")
				}
			}
			return ts.WalkContinue
		})
	}
	return defs
}

// declName pulls the declared identifier out of a function_definition /
// type_definition by descending through declarator/pointer/array/parenthesized
// wrappers to the first identifier / type_identifier.
func declName(n *ts.Node, lang *ts.Language, src []byte) string {
	d := n.ChildByFieldName("declarator", lang)
	return firstIdent(d, lang, src)
}

// firstIdent returns the source text of the first identifier-like descendant.
func firstIdent(n *ts.Node, lang *ts.Language, src []byte) string {
	if n == nil {
		return ""
	}
	stack := []*ts.Node{n}
	for i := 0; i < 64 && len(stack) > 0; i++ {
		cur := stack[0]
		stack = stack[1:]
		switch cur.Type(lang) {
		case "identifier", "type_identifier", "field_identifier":
			return nodeText(cur, src)
		}
		for j := 0; j < cur.NamedChildCount(); j++ {
			if c := cur.NamedChild(j); c != nil {
				stack = append(stack, c)
			}
		}
	}
	return ""
}

func fieldText(n *ts.Node, field string, lang *ts.Language, src []byte) string {
	c := n.ChildByFieldName(field, lang)
	if c == nil {
		return ""
	}
	return nodeText(c, src)
}

// hasBody reports whether an aggregate specifier has a body (field/enumerator
// list) — i.e. it is a definition, not just a type reference.
func hasBody(n *ts.Node, lang *ts.Language) bool {
	for j := 0; j < n.NamedChildCount(); j++ {
		c := n.NamedChild(j)
		if c == nil {
			continue
		}
		switch c.Type(lang) {
		case "field_declaration_list", "enumerator_list":
			return true
		}
	}
	return false
}

func nodeText(n *ts.Node, src []byte) string {
	s, e := n.StartByte(), n.EndByte()
	if int(e) > len(src) || s > e {
		return ""
	}
	return string(src[s:e])
}

func readFile(f string) []byte {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	return b
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
