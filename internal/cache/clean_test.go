package cache

import "testing"

// TestClean_NoSelectorRemovesNothing: Clean with no selector (no --all/--project/
// --older-than) must remove nothing — a guard against accidental mass deletion.
func TestClean_NoSelectorRemovesNothing(t *testing.T) {
	if removed := Clean(CleanOpts{}); len(removed) != 0 {
		t.Errorf("Clean with no selector should remove nothing; got %v", removed)
	}
}

// TestClean_UnknownProjectRemovesNothing: --project pointing at a path with no
// cache matches nothing and removes nothing.
func TestClean_UnknownProjectRemovesNothing(t *testing.T) {
	if removed := Clean(CleanOpts{Project: "/no/such/project/path/xyz"}); len(removed) != 0 {
		t.Errorf("Clean --project on an unknown path should remove nothing; got %v", removed)
	}
}
