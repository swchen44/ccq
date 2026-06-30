package cindex

import "testing"

// lookupKind returns the kind of the first definition of name, or "" if none.
func lookupKind(ix *Index, name string) string {
	defs := ix.Lookup(name)
	if len(defs) == 0 {
		return ""
	}
	return defs[0].Kind
}

func TestDefinitionIndex(t *testing.T) {
	Invalidate()
	ix := Build("testdata")

	// Core goal: a definition behind a never-defined #ifdef must still be found
	// (the scanner does NOT evaluate the preprocessor).
	if got := lookupKind(ix, "hidden_fn"); got != "func" {
		t.Errorf("hidden_fn (behind #ifdef): want kind func, got %q", got)
	}
	if got := lookupKind(ix, "hidden_struct"); got != "struct" {
		t.Errorf("hidden_struct (behind #ifdef): want kind struct, got %q", got)
	}

	// Active definitions of every kind.
	cases := map[string]string{
		"active_fn":         "func",
		"nextline_brace_fn": "func",
		"active_struct":     "struct",
		"active_union":      "union",
		"colors":            "enum",
		"my_typedef_t":      "typedef",
		"my_int_alias":      "typedef",
		"MY_MACRO":          "define",
	}
	for name, want := range cases {
		if got := lookupKind(ix, name); got != want {
			t.Errorf("%s: want kind %q, got %q", name, want, got)
		}
	}

	// Anti-false-positive: a "definition" inside a comment or a string literal
	// must never be indexed.
	if defs := ix.Lookup("comment_fake"); len(defs) != 0 {
		t.Errorf("comment_fake must NOT be indexed (it is inside a comment): %+v", defs)
	}
	if defs := ix.Lookup("string_fake"); len(defs) != 0 {
		t.Errorf("string_fake must NOT be indexed (it is inside a string literal): %+v", defs)
	}
}

func TestLookupLineNumber(t *testing.T) {
	Invalidate()
	ix := Build("testdata")
	defs := ix.Lookup("active_fn")
	if len(defs) != 1 {
		t.Fatalf("active_fn: want 1 def, got %d", len(defs))
	}
	if defs[0].Line != 5 { // line of `int active_fn(int x) { ... }` in defs.c
		t.Errorf("active_fn: want line 5, got %d", defs[0].Line)
	}
}
