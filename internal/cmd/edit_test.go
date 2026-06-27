package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swchen44/ccq/internal/lsp"
)

func rng(sl, sc, el, ec int) lsp.Range {
	return lsp.Range{Start: lsp.Position{Line: sl, Character: sc}, End: lsp.Position{Line: el, Character: ec}}
}

func TestApplyOneSingleLine(t *testing.T) {
	lines := []string{"int foo = 1;"}
	got := applyOne(lines, textEdit{Range: rng(0, 4, 0, 7), NewText: "bar"})
	if got[0] != "int bar = 1;" {
		t.Errorf("single-line edit = %q", got[0])
	}
}

func TestApplyOneMultiLine(t *testing.T) {
	lines := []string{"a(", "  x", ")"}
	got := applyOne(lines, textEdit{Range: rng(0, 0, 2, 1), NewText: "b()"})
	if len(got) != 1 || got[0] != "b()" {
		t.Errorf("multi-line edit = %#v", got)
	}
}

func TestParseWorkspaceEditBothShapes(t *testing.T) {
	changes := `{"changes":{"file:///a.c":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"X"}]}}`
	m := parseWorkspaceEdit(json.RawMessage(changes))
	if len(m) != 1 {
		t.Errorf("changes shape: got %d files", len(m))
	}
	docChanges := `{"documentChanges":[{"textDocument":{"uri":"file:///b.c"},"edits":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}},"newText":"Y"}]}]}`
	m2 := parseWorkspaceEdit(json.RawMessage(docChanges))
	if len(m2) != 1 || len(m2[lsp.URIToPath("file:///b.c")]) != 1 {
		t.Errorf("documentChanges shape: %#v", m2)
	}
}

func TestKindNameMacro(t *testing.T) {
	if kindName(15) != "macro" {
		t.Errorf("kind 15 = %q, want macro", kindName(15))
	}
	if kindName(12) != "function" {
		t.Errorf("kind 12 = %q, want function", kindName(12))
	}
	if kindName(999) != "sym" {
		t.Errorf("unknown kind = %q, want sym", kindName(999))
	}
}

func TestSQLEscape(t *testing.T) {
	if got := sqlEsc("O'Brien"); got != "O''Brien" {
		t.Errorf("sqlEsc = %q", got)
	}
}

func TestWriteSQL(t *testing.T) {
	var b strings.Builder
	writeSQL(&b, []exNode{{Name: "foo", Kind: "function", File: "a.c", Line: 3}},
		[]exEdge{{Src: "bar", Dst: "foo", Kind: "calls"}})
	out := b.String()
	for _, want := range []string{"CREATE TABLE", "INSERT INTO nodes VALUES('foo','function','a.c',3)", "INSERT INTO edges VALUES('bar','foo','calls')"} {
		if !strings.Contains(out, want) {
			t.Errorf("writeSQL missing %q\n%s", want, out)
		}
	}
}
