package cmd

import (
	"path/filepath"
	"testing"
)

// TestApplyOne_OutOfRange: an edit whose range is outside the buffer must be a
// no-op (no panic, no truncation).
func TestApplyOne_OutOfRange(t *testing.T) {
	lines := []string{"only one line"}
	got := applyOne(lines, textEdit{Range: rng(5, 0, 5, 1), NewText: "X"}) // start past EOF
	if len(got) != 1 || got[0] != "only one line" {
		t.Errorf("out-of-range edit should be a no-op; got %#v", got)
	}
	got2 := applyOne(lines, textEdit{Range: rng(-1, 0, 0, 1), NewText: "X"}) // negative start
	if len(got2) != 1 || got2[0] != "only one line" {
		t.Errorf("negative-line edit should be a no-op; got %#v", got2)
	}
	// end line past EOF (multi-line guard)
	got3 := applyOne(lines, textEdit{Range: rng(0, 0, 9, 1), NewText: "X"})
	if len(got3) != 1 || got3[0] != "only one line" {
		t.Errorf("end-past-EOF edit should be a no-op; got %#v", got3)
	}
}

// TestApplyEdits_MissingFile: applying edits to a non-existent file returns an
// error and counts no edits.
func TestApplyEdits_MissingFile(t *testing.T) {
	edits := map[string][]textEdit{
		filepath.Join(t.TempDir(), "nope.c"): {{Range: rng(0, 0, 0, 1), NewText: "X"}},
	}
	n, err := applyEdits(edits)
	if err == nil {
		t.Error("applyEdits on a missing file should return an error")
	}
	if n != 0 {
		t.Errorf("no edits should be counted on read failure; got %d", n)
	}
}

// TestParseWorkspaceEdit_Garbage: malformed or empty workspace-edit JSON yields
// no edits (rather than panicking).
func TestParseWorkspaceEdit_Garbage(t *testing.T) {
	if m := parseWorkspaceEdit([]byte("not json")); len(m) != 0 {
		t.Errorf("garbage input should yield no edits; got %#v", m)
	}
	if m := parseWorkspaceEdit([]byte(`{}`)); len(m) != 0 {
		t.Errorf("empty object should yield no edits; got %#v", m)
	}
	if m := parseWorkspaceEdit([]byte(`{"changes":{}}`)); len(m) != 0 {
		t.Errorf("empty changes should yield no edits; got %#v", m)
	}
}
