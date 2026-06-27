//go:build integration

// Integration tests drive the built ccq binary against a real clangd over a
// tiny C project. Run with:  go test -tags integration ./...
// They are skipped automatically if clangd is not on PATH.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCCQCallersEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH; skipping integration test")
	}
	proj, _ := filepath.Abs("testdata/cproj")

	// Regenerate compile_commands.json with this machine's absolute paths.
	type entry struct {
		Directory string `json:"directory"`
		Command   string `json:"command"`
		File      string `json:"file"`
	}
	var db []entry
	for _, f := range []string{"lib.c", "main.c"} {
		db = append(db, entry{proj, "clang -std=c11 -c " + f, filepath.Join(proj, f)})
	}
	b, _ := json.MarshalIndent(db, "", " ")
	if err := os.WriteFile(filepath.Join(proj, "compile_commands.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the ccq binary to a temp location.
	bin := filepath.Join(t.TempDir(), "ccq")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// add() is called by caller_one and caller_two — function-level callers.
	out, err := exec.Command(bin, "callers", "add", "-p", proj, "--no-daemon").CombinedOutput()
	if err != nil {
		t.Fatalf("ccq callers: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{"caller_one", "caller_two"} {
		if !strings.Contains(got, want) {
			t.Errorf("callers of add missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestCCQSearchEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH; skipping integration test")
	}
	proj, _ := filepath.Abs("testdata/cproj")
	bin := filepath.Join(t.TempDir(), "ccq")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, _ := exec.Command(bin, "search", "add", "-p", proj, "--no-daemon").CombinedOutput()
	if !strings.Contains(string(out), "add") {
		t.Errorf("search add did not find symbol\n%s", out)
	}
}
