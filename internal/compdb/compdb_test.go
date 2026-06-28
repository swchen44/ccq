package compdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStageMerge: --compdb a,b merges the arrays into one compile_commands.json.
func TestStageMerge(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "b1.json")
	b := filepath.Join(root, "b2.json")
	write(t, a, `[{"directory":"/x","command":"clang -c f.c","file":"/x/f.c"}]`)
	write(t, b, `[{"directory":"/y","command":"clang -c g.c","file":"/y/g.c"}]`)

	dir, err := Stage([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "compile_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(out, &entries); err != nil {
		t.Fatalf("staged file is not a JSON array: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("merged should have 2 entries, got %d", len(entries))
	}
	// order is preserved (first --compdb first): clangd uses the first entry for a
	// duplicated file, so b1's entries must precede b2's.
	if len(entries) == 2 && (entries[0]["file"] != "/x/f.c" || entries[1]["file"] != "/y/g.c") {
		t.Errorf("Stage must preserve --compdb order (first wins for duplicate files); got %v", entries)
	}
	// stable dir for the same input set
	dir2, _ := Stage([]string{a, b})
	if dir2 != dir {
		t.Error("Stage dir should be stable for the same input set")
	}
	if d, _ := Stage(nil); d != "" {
		t.Errorf("Stage(nil) should be empty, got %q", d)
	}
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCompileFlags(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "src", "a.c"), "int a(){return 0;}\n")
	write(t, filepath.Join(root, "src", "a.h"), "int a(void);\n")
	write(t, filepath.Join(root, "include", "b.h"), "int b(void);\n")

	if err := writeCompileFlags(root); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "compile_flags.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"-xc", "-std=gnu11", "-Isrc", "-Iinclude"} {
		if !strings.Contains(got, want) {
			t.Errorf("compile_flags.txt missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestLocateAndNoBuild(t *testing.T) {
	// no config -> ""
	root := t.TempDir()
	if d := Locate(root); d != "" {
		t.Errorf("Locate empty project = %q, want \"\"", d)
	}
	// compile_flags only -> no-build
	write(t, filepath.Join(root, "compile_flags.txt"), "-xc\n")
	if d := Locate(root); d != root {
		t.Errorf("Locate = %q, want root", d)
	}
	if !IsNoBuild(root) {
		t.Error("IsNoBuild should be true with only compile_flags.txt")
	}
	// compile_commands wins
	write(t, filepath.Join(root, "compile_commands.json"), "[]\n")
	if IsNoBuild(root) {
		t.Error("IsNoBuild should be false when compile_commands.json present")
	}
}

func TestEnsureFallsBackToCompileFlags(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "main.c"), "int main(){return 0;}\n")
	write(t, filepath.Join(root, "main.h"), "int main(void);\n")
	dir, how, err := Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	if dir != root || how != "compile_flags(no-build)" {
		t.Errorf("Ensure = (%q,%q), want (root, compile_flags(no-build))", dir, how)
	}
}
