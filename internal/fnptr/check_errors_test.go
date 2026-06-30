package fnptr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheck_BadJSON: a malformed ccq.fnptr.json surfaces as an error from Check
// (and LoadTable), not a panic or a silent success.
func TestCheck_BadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ccq.fnptr.json"), []byte(`{ "links": [ `), 0o644); err != nil {
		t.Fatal(err)
	}
	found, _, err := Check(dir)
	if err == nil {
		t.Fatal("Check should error on malformed ccq.fnptr.json")
	}
	if found {
		t.Error("found should be false when the table fails to parse")
	}
}

// TestLoadTable_BadJSON: LoadTable reports the parse error and the offending path.
func TestLoadTable_BadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ccq.fnptr.json"), []byte(`not json at all`), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, p, err := LoadTable(dir)
	if err == nil {
		t.Fatal("LoadTable should error on invalid JSON")
	}
	if tbl != nil {
		t.Error("LoadTable should return a nil table on error")
	}
	if !strings.Contains(p, "ccq.fnptr.json") {
		t.Errorf("error path should name the table file; got %q", p)
	}
}

// TestCheck_UndefinedHandler: a registration to a real struct/field whose handler
// isn't defined anywhere in the project warns about the missing handler.
func TestCheck_UndefinedHandler(t *testing.T) {
	dir := t.TempDir()
	src := "struct ops { int (*scan)(void); };\nint realh(void){ return 0; }\n"
	if err := os.WriteFile(filepath.Join(dir, "x.c"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "ccq.fnptr.json"), []byte(`{
		"registrations": [
			{ "struct": "ops", "field": "scan", "handlers": ["ghost_handler"] }
		]
	}`), 0o644)
	Invalidate()
	_, warnings, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "ghost_handler") {
		t.Errorf("expected a warning about the undefined handler; got %v", warnings)
	}
}
