package compdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swchen44/ccq/internal/config"
)

// resetConfig clears any ccq.json filter loaded by a test so later tests in this
// package (which use config.Keep via writeCompileFlags/Ensure) see "keep all".
func resetConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { config.Load(t.TempDir(), "") })
}

// TestStage_MissingFile: a --compdb path that doesn't exist returns an error, not
// a silent empty stage.
func TestStage_MissingFile(t *testing.T) {
	_, err := Stage([]string{filepath.Join(t.TempDir(), "nope.json")})
	if err == nil {
		t.Fatal("Stage on a non-existent --compdb file should error")
	}
}

// TestStage_NotAnArray: a JSON file that is not a compile_commands.json array
// (e.g. an object) yields a clear error.
func TestStage_NotAnArray(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(p, []byte(`{"directory":"/x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Stage([]string{p})
	if err == nil || !strings.Contains(err.Error(), "not a compile_commands.json array") {
		t.Fatalf("Stage on a non-array should report a clear error; got %v", err)
	}
}

// TestStage_EmptyArray: an empty array is valid and merges into an empty DB.
func TestStage_EmptyArray(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(p, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := Stage([]string{p})
	if err != nil {
		t.Fatalf("Stage([]) should succeed; got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var arr []json.RawMessage
	if json.Unmarshal(b, &arr) != nil || len(arr) != 0 {
		t.Errorf("staged empty DB should be an empty array; got %s", b)
	}
}

// TestApplyFilter_NoConfig: with no ccq.json loaded, ApplyFilter is a no-op.
func TestApplyFilter_NoConfig(t *testing.T) {
	resetConfig(t)
	config.Load(t.TempDir(), "") // no ccq.json -> Source()==""
	if got := ApplyFilter("/some/dir"); got != "/some/dir" {
		t.Errorf("no config: ApplyFilter should return ccDir unchanged; got %q", got)
	}
}

// TestApplyFilter_NoBuildDirUnchanged: a config is active but ccDir has no
// compile_commands.json (no-build mode) — ApplyFilter returns it unchanged.
func TestApplyFilter_NoBuildDirUnchanged(t *testing.T) {
	resetConfig(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ccq.json"), []byte(`{"deny":["x"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Load(root, "")
	ccDir := t.TempDir() // empty: no compile_commands.json
	if got := ApplyFilter(ccDir); got != ccDir {
		t.Errorf("no-build ccDir should be returned unchanged; got %q", got)
	}
}

// TestApplyFilter_RemovesDenied: with a deny filter, ApplyFilter stages a copy of
// the compile DB with the denied file removed.
func TestApplyFilter_RemovesDenied(t *testing.T) {
	resetConfig(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ccq.json"), []byte(`{"deny":["main\\.c$"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Load(root, "")
	db := []map[string]string{
		{"directory": root, "command": "clang -c lib.c", "file": filepath.Join(root, "lib.c")},
		{"directory": root, "command": "clang -c main.c", "file": filepath.Join(root, "main.c")},
	}
	b, _ := json.Marshal(db)
	if err := os.WriteFile(filepath.Join(root, "compile_commands.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	out := ApplyFilter(root)
	if out == root {
		t.Fatal("ApplyFilter should stage a filtered copy when a file is denied")
	}
	fb, err := os.ReadFile(filepath.Join(out, "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(fb)
	if strings.Contains(s, "main.c") {
		t.Errorf("denied main.c should be removed from the staged DB:\n%s", s)
	}
	if !strings.Contains(s, "lib.c") {
		t.Errorf("kept lib.c should remain in the staged DB:\n%s", s)
	}
}

// TestApplyFilter_NothingDenied: a deny pattern that matches nothing leaves the
// DB intact, so ApplyFilter returns ccDir unchanged (no needless restage).
func TestApplyFilter_NothingDenied(t *testing.T) {
	resetConfig(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ccq.json"), []byte(`{"deny":["zzz_no_match"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Load(root, "")
	db := []map[string]string{{"directory": root, "command": "clang -c a.c", "file": filepath.Join(root, "a.c")}}
	b, _ := json.Marshal(db)
	if err := os.WriteFile(filepath.Join(root, "compile_commands.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ApplyFilter(root); got != root {
		t.Errorf("nothing denied: ApplyFilter should return ccDir unchanged; got %q", got)
	}
}
