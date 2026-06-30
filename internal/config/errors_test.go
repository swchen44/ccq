package config

import (
	"strings"
	"testing"
)

// TestBadJSONFailOpen: a ccq.json that is not valid JSON must not crash; it
// records a warning and leaves the filter "match nothing" (everything kept).
func TestBadJSONFailOpen(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "deny": [ }`) // malformed JSON
	Load(root, "")
	if len(Warnings()) == 0 {
		t.Error("expected a warning for malformed JSON")
	}
	if Source() != "" {
		t.Error("malformed JSON should leave Source() empty (no usable settings)")
	}
	if !Keep(root + "/anything.c") {
		t.Error("malformed JSON should fail open (keep everything)")
	}
}

// TestEmptyFileFailOpen: a zero-byte ccq.json is invalid JSON; same fail-open
// behavior as malformed content.
func TestEmptyFileFailOpen(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, ``) // empty file
	Load(root, "")
	if len(Warnings()) == 0 {
		t.Error("expected a warning for an empty (invalid JSON) ccq.json")
	}
	if !Keep(root + "/x.c") {
		t.Error("empty ccq.json should fail open (keep everything)")
	}
}

// TestEmptyObjectKeepsAll: `{}` is valid JSON with no allow/deny — a loaded but
// empty filter keeps everything, with no warnings.
func TestEmptyObjectKeepsAll(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{}`)
	Load(root, "")
	if Source() == "" {
		t.Error("`{}` is valid JSON; Source() should be set")
	}
	if len(Warnings()) != 0 {
		t.Errorf("empty object should produce no warnings; got %v", Warnings())
	}
	if !Keep(root + "/x.c") {
		t.Error("no allow/deny means keep everything")
	}
}

// TestAllowAndDenyBothMatch: when a file matches both allow and deny, deny wins.
func TestAllowAndDenyBothMatch(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "allow": ["^src/"], "deny": ["secret"] }`)
	Load(root, "")
	if Keep(root + "/src/secret.c") {
		t.Error("a file matching both allow and deny must be denied (deny wins)")
	}
	if !Keep(root + "/src/ok.c") {
		t.Error("allowed, non-denied file should be kept")
	}
}

// TestBadAllowRegexPartial: an invalid allow regex is dropped (with a warning)
// but the remaining valid allow patterns still restrict the index.
func TestBadAllowRegexPartial(t *testing.T) {
	root := t.TempDir()
	writeCfg(t, root, `{ "allow": ["[unclosed", "^src/"] }`)
	Load(root, "")
	if len(Warnings()) == 0 {
		t.Error("expected a warning for the invalid allow regex")
	}
	if !strings.Contains(strings.Join(Warnings(), "\n"), "allow") {
		t.Errorf("warning should name the allow kind; got %v", Warnings())
	}
	if !Keep(root + "/src/a.c") {
		t.Error("valid allow ^src/ should still keep src files")
	}
	if Keep(root + "/other/b.c") {
		t.Error("a valid allow pattern should still exclude non-matching files")
	}
}
