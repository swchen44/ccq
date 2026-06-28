package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	old := Entry{Project: "/p", Modified: time.Now().Add(-48 * time.Hour)}
	fresh := Entry{Project: "/p", Modified: time.Now()}

	if !match(old, CleanOpts{All: true}) || !match(fresh, CleanOpts{All: true}) {
		t.Error("--all should match everything")
	}
	if !match(old, CleanOpts{Project: "/p"}) || match(old, CleanOpts{Project: "/other"}) {
		t.Error("--project should match by root")
	}
	if !match(old, CleanOpts{OlderThan: 24 * time.Hour}) {
		t.Error("48h-old entry should match --older-than 24h")
	}
	if match(fresh, CleanOpts{OlderThan: 24 * time.Hour}) {
		t.Error("fresh entry should NOT match --older-than 24h")
	}
	if match(old, CleanOpts{}) {
		t.Error("no selector should match nothing")
	}
}

func TestDirStat(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(d, "b"), []byte("world!"), 0o644)
	sz, mt := dirStat(d)
	if sz != 11 {
		t.Errorf("size = %d, want 11", sz)
	}
	if mt.IsZero() {
		t.Error("expected a non-zero newest mtime")
	}
}
