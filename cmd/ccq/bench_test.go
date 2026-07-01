//go:build integration

// Benchmark-ground-truth regression tests: they pin the known real-repo answers
// that back ccq's headline differentiators (fn-pointer dispatch recall + direct
// call-graph recall) so a refactor can't silently regress them. The reference
// repos live outside this module (the cbm-vs-codegraph-bench harness), so each
// test SKIPS when the repo isn't present. Point CCQ_BENCH_REPOS at the harness's
// `repos/` dir, or rely on the default path.
//
// Run with:  go test -tags integration -run TestBench ./cmd/ccq
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swchen44/ccq/internal/fnptr"
)

// benchRepo returns the path to repo <name> under the bench harness's repos dir,
// or "" (with a skip) when it isn't available.
func benchRepo(t *testing.T, name string) string {
	t.Helper()
	base := os.Getenv("CCQ_BENCH_REPOS")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "git", "cbm-vs-codegraph-bench", "repos")
	}
	p := filepath.Join(base, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("bench repo %q not found at %s (set CCQ_BENCH_REPOS); skipping", name, p)
	}
	return p
}

// wpa_supplicant registers 5 handlers to `struct wpa_driver_ops`.scan2. ccq's
// fn-pointer synthesizer (pure text, #ifdef-blind) must recover ALL five — the
// benchmark headline: ccq 5/5 vs CodeGraph 3/5 vs cbm 0/5. This exercises the
// synthesizer directly (no clangd needed), so it is fast and deterministic.
func TestBenchWpaScan2Recall(t *testing.T) {
	wpa := benchRepo(t, "wpa_supplicant")
	handlers := []string{
		"driver_nl80211_scan2",
		"wpa_driver_bsd_scan",  // return type on previous line (col-0 name)
		"wpa_driver_ndis_scan", // imperative `ops.scan2 = ...;` assignment
		"wpa_driver_privsep_scan",
		"wpa_driver_wext_scan",
	}
	fnptr.Invalidate()
	hit := 0
	for _, h := range handlers {
		bridged := false
		for _, c := range fnptr.Callers(wpa, h) {
			if strings.Contains(c.Field, "scan2") {
				bridged = true
				break
			}
		}
		if bridged {
			hit++
		} else {
			t.Errorf("%s: no scan2 dispatcher bridged (fnptr recall miss)", h)
		}
	}
	if hit != len(handlers) {
		t.Errorf("wpa .scan2 fnptr recall = %d/%d, want %d/%d", hit, len(handlers), len(handlers), len(handlers))
	}
}

// redis: lookupCommand has 13 direct callers (clangd call hierarchy ground
// truth) — the direct-call-graph dimension where cbm scored 0. Drives the built
// binary end-to-end against a real clangd.
func TestBenchRedisLookupCommandCallers(t *testing.T) {
	if _, err := exec.LookPath("clangd"); err != nil {
		t.Skip("clangd not on PATH; skipping")
	}
	redis := benchRepo(t, "redis")
	bin := filepath.Join(t.TempDir(), "ccq")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build ccq: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "callers", "lookupCommand", "-p", redis, "--no-daemon").CombinedOutput()
	if err != nil {
		t.Fatalf("ccq callers: %v\n%s", err, out)
	}
	n := 0
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(ln, "  ") { // caller lines are indented
			n++
		}
	}
	if n != 13 {
		t.Errorf("redis lookupCommand callers = %d, want 13\n--- output ---\n%s", n, out)
	}
}
