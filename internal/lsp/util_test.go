package lsp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tmpFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.c")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSnippetAndLines(t *testing.T) {
	f := tmpFile(t, "l0\n  l1  \nl2\nl3\n")
	if got := Snippet(f, 1, 2); got != "  l1  \nl2" {
		t.Errorf("Snippet = %q", got)
	}
	if got := LineText(f, 1); got != "l1" { // trimmed
		t.Errorf("LineText = %q, want l1", got)
	}
	if got := LineText0(f, 1); got != "  l1  " { // raw
		t.Errorf("LineText0 = %q", got)
	}
	if got := LineText(f, 999); got != "" {
		t.Errorf("out-of-range LineText = %q, want empty", got)
	}
}

func TestIsCpp(t *testing.T) {
	for _, f := range []string{"a.cpp", "b.hpp", "c.cc"} {
		if !isCpp(f) {
			t.Errorf("%s should be cpp", f)
		}
	}
	for _, f := range []string{"a.c", "b.h"} {
		if isCpp(f) {
			t.Errorf("%s should not be cpp", f)
		}
	}
}

func TestURIRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix path assumptions")
	}
	p := "/tmp/some dir/file.c"
	uri := pathToURI(p)
	if uri != "file:///tmp/some dir/file.c" {
		t.Errorf("pathToURI = %q", uri)
	}
	if back := URIToPath(uri); back != p {
		t.Errorf("URIToPath(%q) = %q, want %q", uri, back, p)
	}
}
