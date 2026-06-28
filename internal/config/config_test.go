package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCfg(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "ccq.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKeepAllowDeny(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "allow": ["^src/"], "deny": ["_test\\.c$"] }`)
	Load(root, "")
	if Source() == "" {
		t.Fatal("config not loaded")
	}
	cases := map[string]bool{
		"src/a.c":      true,  // matches allow, not deny
		"src/a_test.c": false, // denied
		"other/b.c":    false, // not in allow
	}
	for rel, want := range cases {
		if got := Keep(filepath.Join(root, rel)); got != want {
			t.Errorf("Keep(%s) = %v, want %v", rel, got, want)
		}
	}
}

func TestNoConfigKeepsAll(t *testing.T) {
	Load(t.TempDir(), "") // no ccq.json
	if Source() != "" {
		t.Error("expected no source")
	}
	if !Keep("/anywhere/x.c") {
		t.Error("with no config, everything should be kept")
	}
}

func TestBadRegexFailOpen(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "deny": ["[unclosed"] }`)
	Load(root, "")
	if len(Warnings()) == 0 {
		t.Error("expected a warning for the bad regex")
	}
	if !Keep(filepath.Join(root, "x.c")) {
		t.Error("bad regex should fail open (keep), not block everything")
	}
}

func TestKeyChangesWithContent(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "deny": ["a"] }`)
	Load(root, "")
	k1 := Key()
	writeCfg(t, root, `{ "deny": ["b"] }`)
	Load(root, "")
	if Key() == k1 || k1 == "" {
		t.Errorf("Key should differ when content changes (k1=%q k2=%q)", k1, Key())
	}
}
