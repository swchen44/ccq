// Package gitdiff reports source files changed between git revisions, so the
// daemon can prioritise re-indexing edited code on a warm restart. It shells
// out to git and degrades gracefully (returns nothing) when git is unavailable
// or the directory is not a repository — staying zero-dependency.
package gitdiff

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func isSource(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hh", ".hxx":
		return true
	}
	return false
}

// Head returns the current commit hash, or "" if root is not a git repo.
func Head(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ChangedSince returns absolute paths of source files that changed since rev
// (committed diff + uncommitted working-tree changes). Returns nil on any error.
func ChangedSince(root, rev string) []string {
	if rev == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = strings.TrimSpace(rel)
		if rel == "" || !isSource(rel) {
			return
		}
		abs := filepath.Join(root, rel)
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	// committed changes rev..HEAD
	if b, err := exec.Command("git", "-C", root, "diff", "--name-only", rev+"..HEAD").Output(); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			add(l)
		}
	}
	// uncommitted working-tree changes
	if b, err := exec.Command("git", "-C", root, "status", "--porcelain").Output(); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if len(l) > 3 {
				add(l[3:])
			}
		}
	}
	return out
}
