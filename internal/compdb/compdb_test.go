package compdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
