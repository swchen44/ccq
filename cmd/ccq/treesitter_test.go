//go:build integration

// End-to-end tests for the opt-in --treesitter definition-index backend. Built
// with the grammar tags so the C grammar is present (as release binaries are).
// Run with:  go test -tags integration -run TestTreeSitter ./cmd/ccq
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTaggedCCQ builds a ccq binary with the tree-sitter C grammar linked in.
func buildTaggedCCQ(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ccq")
	out, err := exec.Command("go", "build", "-tags", "grammar_subset grammar_subset_c", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build ccq (tagged): %v\n%s", err, out)
	}
	return bin
}

// A project whose symbols all sit behind a never-defined #ifdef, so clangd
// (no -D) drops them and the text-index fallback is exercised. An iterator macro
// sits between two of them to trigger the tree-sitter cascade.
const tsProj = `int visible_fn(int x){ return x; }
#ifdef NOPE_NOT_DEFINED
int hidden_early(int y){ return y; }
static int iter_fn(void *l){ foreach_thing(a, l, i) { use(a); }; return 0; }
int hidden_after_iter(void){ return 7; }
#endif
`

func writeTSProj(t *testing.T) string {
	t.Helper()
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "m.c"), []byte(tsProj), 0644); err != nil {
		t.Fatal(err)
	}
	return proj
}

func def(t *testing.T, bin, proj, sym string, treesitter bool) string {
	t.Helper()
	args := []string{"def", sym, "-p", proj, "--no-daemon"}
	if treesitter {
		args = append(args, "--treesitter")
	}
	out, _ := exec.Command(bin, args...).CombinedOutput()
	return string(out)
}

// The tree-sitter backend is #ifdef-blind and correctly labelled: a symbol before
// the iterator macro (but behind a disabled #ifdef) is found via --treesitter.
func TestTreeSitterFindsIfdefHidden(t *testing.T) {
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH")
	}
	bin := buildTaggedCCQ(t)
	proj := writeTSProj(t)
	out := def(t, bin, proj, "hidden_early", true)
	if !strings.Contains(out, "hidden_early") || !strings.Contains(out, "tree-sitter") {
		t.Errorf("--treesitter should find hidden_early and label it tree-sitter; got:\n%s", out)
	}
}

// KNOWN LIMITATION, pinned end-to-end: a definition AFTER an iterator macro is
// dropped by the tree-sitter cascade, but the default (regex) backend still finds
// it. This is exactly why --treesitter is opt-in and off by default.
func TestTreeSitterIteratorMacroCascade(t *testing.T) {
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH")
	}
	bin := buildTaggedCCQ(t)
	proj := writeTSProj(t)

	// default regex backend finds the symbol after the iterator macro
	if reg := def(t, bin, proj, "hidden_after_iter", false); !strings.Contains(reg, "hidden_after_iter:") && !strings.Contains(reg, "[func]") {
		t.Errorf("default (regex) backend should find hidden_after_iter; got:\n%s", reg)
	}
	// --treesitter drops it (cascade) -> symbol not found
	if ts := def(t, bin, proj, "hidden_after_iter", true); !strings.Contains(ts, "symbol not found") {
		t.Logf("NOTE: --treesitter found hidden_after_iter (cascade no longer happens?):\n%s", ts)
		t.Skip("iterator-macro cascade no longer drops the symbol — upstream improvement")
	}
}
