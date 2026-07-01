package tsindex

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

// skipIfNoGrammar skips when the C grammar wasn't compiled in (no
// `-tags 'grammar_subset grammar_subset_c'`), so plain `go test ./...` passes.
func skipIfNoGrammar(t *testing.T) {
	t.Helper()
	if grammars.DetectLanguageByName("c") == nil {
		t.Skip("tree-sitter C grammar not compiled in; run: go test -tags 'grammar_subset grammar_subset_c' ./internal/tsindex/")
	}
}

func kindOf(name string) string {
	defs := Lookup("testdata", name)
	if len(defs) == 0 {
		return ""
	}
	return defs[0].Kind
}

func TestTreeSitterDefinitions(t *testing.T) {
	skipIfNoGrammar(t)
	Invalidate()
	cases := map[string]string{
		"active_fn":  "func",
		"hidden_fn":  "func",   // inside #ifdef NEVER_DEFINED -> #ifdef-blind
		"hidden_s":   "struct", // ditto
		"active_s":   "struct",
		"active_u":   "union",
		"colors":     "enum",
		"my_t":       "typedef",
		"kr_name_fn": "func", // K&R column-0 name (return type on previous line)
	}
	for name, want := range cases {
		if got := kindOf(name); got != want {
			t.Errorf("%s: want kind %q, got %q", name, want, got)
		}
	}
}

// KNOWN LIMITATION (visible, by design): a single iterator/control-flow macro
// makes tree-sitter cascade and drop every definition after it in the file. This
// is exactly why --treesitter is opt-in and off by default. See
// docs/tree-sitter-exploration.md.
func TestIteratorMacroCascade(t *testing.T) {
	skipIfNoGrammar(t)
	Invalidate()
	if kindOf("iter_before") != "func" {
		t.Error("iter_before (before the iterator macro) should be found")
	}
	if len(Lookup("testdata", "iter_after")) == 0 {
		t.Skip("KNOWN LIMITATION: an iterator macro `foreach(...) { ... };` makes tree-sitter cascade and drop iter_after — this is why --treesitter is off by default (docs/tree-sitter-exploration.md)")
	}
	// Reaching here means the cascade no longer happens (an upstream improvement).
}
