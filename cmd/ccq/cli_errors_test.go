//go:build integration

// Non-happy-path CLI tests: unknown commands, missing args, bad --compdb,
// degraded mode, dry-run safety, and exit-code/stderr contracts. They reuse the
// build + project helpers from regression_test.go (ccqbin/cproj/copyCproj/runOE)
// and add runCode for asserting the process exit code.
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCode runs the binary and returns stdout, stderr, and the process exit code.
func runCode(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var o, e bytes.Buffer
	c := exec.Command(bin, args...)
	c.Stdout, c.Stderr = &o, &e
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return o.String(), e.String(), code
}

// Unknown subcommand: exit 1 + a clear "unknown command" message + usage.
func TestCLIUnknownCommand(t *testing.T) {
	bin := ccqbin(t)
	out, _, code := runCode(t, bin, "frobnicate")
	if code != 1 {
		t.Errorf("unknown command exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "unknown command: frobnicate") {
		t.Errorf("expected an 'unknown command' message; got:\n%s", out)
	}
}

// No args at all: exit 1 + usage.
func TestCLINoArgs(t *testing.T) {
	bin := ccqbin(t)
	out, _, code := runCode(t, bin)
	if code != 1 {
		t.Errorf("no-args exit code = %d, want 1", code)
	}
	if !strings.Contains(out, "USAGE") {
		t.Errorf("expected usage text on no args; got:\n%s", out)
	}
}

// --compdb pointing at a non-existent file: exit 1 with a --compdb-scoped error.
func TestCLICompdbMissingFile(t *testing.T) {
	bin := ccqbin(t)
	proj := cproj(t)
	missing := filepath.Join(t.TempDir(), "nope.json")
	_, stderr, code := runCode(t, bin, "callers", "add", "--compdb", missing, "-p", proj, "--no-daemon")
	if code != 1 {
		t.Errorf("--compdb missing-file exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "--compdb") {
		t.Errorf("expected a --compdb error on stderr; got:\n%s", stderr)
	}
}

// --compdb pointing at JSON that isn't a compile-commands array: exit 1 + a clear
// "not a compile_commands.json array" message.
func TestCLICompdbMalformedJSON(t *testing.T) {
	bin := ccqbin(t)
	proj := cproj(t)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"not":"an array"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCode(t, bin, "callers", "add", "--compdb", bad, "-p", proj, "--no-daemon")
	if code != 1 {
		t.Errorf("--compdb malformed exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not a compile_commands.json array") {
		t.Errorf("expected a clear 'not an array' error; got:\n%s", stderr)
	}
}

// A project with no compile_commands.json and no compile_flags.txt must print the
// degraded (same-file) mode warning on stderr.
func TestCLIDegradedModeWarning(t *testing.T) {
	bin := ccqbin(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.c"),
		[]byte("static int foo(void){ return 0; }\nint bar(void){ return foo(); }\n"), 0o644)
	_, stderr := runOE(t, bin, "callers", "foo", "-p", dir, "--no-daemon")
	if !strings.Contains(stderr, "degraded") {
		t.Errorf("expected a degraded-mode warning on stderr; got:\n%s", stderr)
	}
}

// def on a symbol that does not exist: not an error exit, but a clear message.
func TestCLIDefSymbolNotFound(t *testing.T) {
	bin := ccqbin(t)
	proj := cproj(t)
	out, _, code := runCode(t, bin, "def", "no_such_symbol_xyz", "-p", proj, "--no-daemon")
	if code != 0 {
		t.Errorf("not-found should not be an error exit; code = %d, want 0", code)
	}
	if !strings.Contains(out, "symbol not found") {
		t.Errorf("expected a 'symbol not found' message; got:\n%s", out)
	}
}

// replace-body without a content-file argument: usage message, exit 0.
func TestCLIReplaceBodyMissingArg(t *testing.T) {
	bin := ccqbin(t)
	proj := cproj(t)
	out, _, code := runCode(t, bin, "replace-body", "add", "-p", proj, "--no-daemon")
	if code != 0 {
		t.Errorf("replace-body missing content-file exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "usage: ccq replace-body") {
		t.Errorf("expected a usage message; got:\n%s", out)
	}
}

// replace-body with a content file that doesn't exist: a read error, and the
// target file must be left byte-for-byte unchanged even with --apply.
func TestCLIReplaceBodyContentFileNotFound(t *testing.T) {
	bin := ccqbin(t)
	proj := copyCproj(t)
	before, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	missing := filepath.Join(t.TempDir(), "body.txt")
	out, _, code := runCode(t, bin, "replace-body", "add", missing, "-p", proj, "--no-daemon", "--apply")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "read") {
		t.Errorf("expected a read-error message; got:\n%s", out)
	}
	after, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	if !bytes.Equal(before, after) {
		t.Error("lib.c must be unchanged when the content file is missing")
	}
}

// Dry-run rename (no --apply) must not modify any file on disk.
func TestCLIRenameDryRunDoesNotModify(t *testing.T) {
	bin := ccqbin(t)
	proj := copyCproj(t)
	libBefore, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	mainBefore, _ := os.ReadFile(filepath.Join(proj, "main.c"))
	out, _, code := runCode(t, bin, "rename", "add", "plus", "-p", proj, "--no-daemon")
	if code != 0 {
		t.Errorf("rename dry-run exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected a dry-run notice; got:\n%s", out)
	}
	libAfter, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	mainAfter, _ := os.ReadFile(filepath.Join(proj, "main.c"))
	if !bytes.Equal(libBefore, libAfter) || !bytes.Equal(mainBefore, mainAfter) {
		t.Error("dry-run rename must not modify any file")
	}
}

// Dry-run replace-body (no --apply) must not modify the target file.
func TestCLIReplaceBodyDryRunDoesNotModify(t *testing.T) {
	bin := ccqbin(t)
	proj := copyCproj(t)
	before, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	body := filepath.Join(t.TempDir(), "body.txt")
	os.WriteFile(body, []byte("int add(int a, int b){ return a + b + 99; }"), 0o644)
	out, _, code := runCode(t, bin, "replace-body", "add", body, "-p", proj, "--no-daemon")
	if code != 0 {
		t.Errorf("replace-body dry-run exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected a dry-run notice; got:\n%s", out)
	}
	after, _ := os.ReadFile(filepath.Join(proj, "lib.c"))
	if !bytes.Equal(before, after) {
		t.Error("dry-run replace-body must not modify lib.c")
	}
}

// cache clean with no selector: removes nothing, with a clear message.
func TestCLICacheCleanNoSelector(t *testing.T) {
	bin := ccqbin(t)
	out, _, code := runCode(t, bin, "cache", "clean")
	if code != 0 {
		t.Errorf("cache clean (no selector) exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "nothing selected") {
		t.Errorf("expected a 'nothing selected' message; got:\n%s", out)
	}
}

// cache clean --project on a path with no cache: removes nothing.
func TestCLICacheCleanUnknownProject(t *testing.T) {
	bin := ccqbin(t)
	out, _, code := runCode(t, bin, "cache", "clean", "--project", filepath.Join(t.TempDir(), "nope"))
	if code != 0 {
		t.Errorf("cache clean --project exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "freed 0B") {
		t.Errorf("expected 'freed 0B'; got:\n%s", out)
	}
}
