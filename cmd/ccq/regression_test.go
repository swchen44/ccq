//go:build integration

// Regression tests for the bugs documented in docs/case-studies/bugs-found.md.
// Each test pins a bug that real-repo case studies surfaced. Run with:
//
//	go test -tags integration ./...
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	sharedBin string
	buildErr  string
)

// ccqbin lazily builds the binary once for all regression tests.
func ccqbin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH; skipping integration regression test")
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ccq-regr")
		if err != nil {
			buildErr = err.Error()
			return
		}
		sharedBin = filepath.Join(dir, "ccq")
		if out, err := exec.Command("go", "build", "-o", sharedBin, ".").CombinedOutput(); err != nil {
			buildErr = err.Error() + "\n" + string(out)
		}
	})
	if buildErr != "" {
		t.Fatalf("build ccq: %s", buildErr)
	}
	return sharedBin
}

type ccEntry struct {
	Directory string `json:"directory"`
	Command   string `json:"command"`
	File      string `json:"file"`
}

func writeCompileCommands(t *testing.T, dir string, files ...string) {
	t.Helper()
	var db []ccEntry
	for _, f := range files {
		db = append(db, ccEntry{dir, "clang -std=c11 -c " + f, filepath.Join(dir, f)})
	}
	b, _ := json.MarshalIndent(db, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "compile_commands.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// cproj returns the shared testdata/cproj with a fresh compile_commands.json
// (read-only tests). lib.c defines add; main.c has caller_one/caller_two calling it.
func cproj(t *testing.T) string {
	t.Helper()
	proj, _ := filepath.Abs("testdata/cproj")
	writeCompileCommands(t, proj, "lib.c", "main.c")
	return proj
}

// copyCproj copies cproj into a temp dir (for tests that mutate files).
func copyCproj(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src, _ := filepath.Abs("testdata/cproj")
	for _, f := range []string{"lib.h", "lib.c", "main.c"} {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dst, f), b, 0o644)
	}
	writeCompileCommands(t, dst, "lib.c", "main.c")
	return dst
}

func run(t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, _ := exec.Command(bin, args...).CombinedOutput()
	return string(out)
}

func runOE(t *testing.T, bin string, args ...string) (stdout, stderr string) {
	t.Helper()
	var o, e bytes.Buffer
	c := exec.Command(bin, args...)
	c.Stdout, c.Stderr = &o, &e
	c.Run()
	return o.String(), e.String()
}

// Bug #1 — explore/callees must use the body-scan + fnptr path, not clangd's
// unreliable outgoingCalls. caller_one calls add, so callees must list it.
func TestRegrCalleesBodyScan(t *testing.T) {
	bin := ccqbin(t)
	out := run(t, bin, "callees", "caller_one", "-p", cproj(t), "--no-daemon")
	if !strings.Contains(out, "add") {
		t.Errorf("bug #1 regressed: callees of caller_one should include add\n%s", out)
	}
}

// Bug #2 — def/explore must show the .c definition body, not a header prototype
// (clangd go-to-definition can jump definition->declaration).
func TestRegrDefShowsDefinition(t *testing.T) {
	bin := ccqbin(t)
	out := run(t, bin, "def", "add", "-p", cproj(t), "--no-daemon")
	if !strings.Contains(out, "lib.c") || !strings.Contains(out, "{") {
		t.Errorf("bug #2 regressed: def add should show the lib.c definition body, not lib.h\n%s", out)
	}
}

// Bug #4 — export --focus builds a bounded neighborhood (not the whole repo).
func TestRegrExportFocus(t *testing.T) {
	bin := ccqbin(t)
	out := run(t, bin, "export", "--format", "json", "--focus", "add", "-p", cproj(t), "--no-daemon")
	for _, want := range []string{`"focus": "add"`, "caller_one", "caller_two"} {
		if !strings.Contains(out, want) {
			t.Errorf("bug #4 regressed: export --focus add missing %q\n%s", want, out)
		}
	}
}

// Bug #6 — symbols line numbers must come from location.range (flat
// SymbolInformation), not a top-level range (which was always 0 -> L1).
func TestRegrSymbolsLineNumbers(t *testing.T) {
	bin := ccqbin(t)
	mainC, _ := filepath.Abs("testdata/cproj/main.c") // caller_one is on line 2 (#include is line 1)
	out := run(t, bin, "symbols", mainC, "-p", cproj(t), "--no-daemon")
	if !strings.Contains(out, "caller_one") {
		t.Fatalf("no caller_one in symbols output\n%s", out)
	}
	if strings.Contains(out, "caller_one\t[function]\tL1") {
		t.Errorf("bug #6 regressed: caller_one should be L2, not L1\n%s", out)
	}
}

// Bug #7 — after rename --apply, the SAME warm daemon must see the new symbol
// (it used to serve the pre-edit index). Uses the daemon path (not --no-daemon).
func TestRegrDaemonSyncAfterApply(t *testing.T) {
	bin := ccqbin(t)
	proj := copyCproj(t)
	defer run(t, bin, "shutdown", "-p", proj)
	run(t, bin, "rename", "add", "plus", "--apply", "-p", proj)
	out := run(t, bin, "callers", "plus", "-p", proj)
	if strings.Contains(out, "(none)") || !strings.Contains(out, "caller_one") {
		t.Errorf("bug #7 regressed: daemon should see callers of plus after rename --apply\n%s", out)
	}
}

// --compdb must accept a renamed compile database and drive clangd through it
// (no compile_commands.json in the source root).
func TestCompdbNamedFile(t *testing.T) {
	bin := ccqbin(t)
	proj := copyCproj(t)
	os.Remove(filepath.Join(proj, "compile_commands.json")) // only a renamed DB exists
	db := filepath.Join(proj, "build1.json")
	b, _ := json.MarshalIndent([]ccEntry{
		{proj, "clang -std=c11 -c lib.c", filepath.Join(proj, "lib.c")},
		{proj, "clang -std=c11 -c main.c", filepath.Join(proj, "main.c")},
	}, "", " ")
	os.WriteFile(db, b, 0o644)
	out := run(t, bin, "callers", "add", "--compdb", db, "-p", proj, "--no-daemon")
	if !strings.Contains(out, "caller_one") {
		t.Errorf("--compdb (renamed DB) should resolve callers of add\n%s", out)
	}
	if strings.Contains(out, "no-build mode") {
		t.Errorf("--compdb provides a real DB; no-build warning should not appear\n%s", out)
	}
}

// Bug #8 — the no-build warning must print in the default daemon path, not only
// with --no-daemon.
func TestRegrNoBuildWarningInDaemon(t *testing.T) {
	bin := ccqbin(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.c"), []byte("int a(void){ return 0; }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "compile_flags.txt"), []byte("-xc\n-std=gnu11\n"), 0o644)
	defer run(t, bin, "shutdown", "-p", dir)
	_, stderr := runOE(t, bin, "def", "a", "-p", dir) // daemon mode (default)
	if !strings.Contains(stderr, "no-build mode") {
		t.Errorf("bug #8 regressed: no-build warning should print in daemon mode\nstderr: %s", stderr)
	}
}
