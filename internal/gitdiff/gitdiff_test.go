package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestChangedSince(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	os.WriteFile(filepath.Join(root, "a.c"), []byte("int a(){return 0;}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("hi\n"), 0o644)
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "init")
	base := Head(root)
	if base == "" {
		t.Fatal("Head returned empty after commit")
	}

	// modify a source file and a non-source file
	os.WriteFile(filepath.Join(root, "a.c"), []byte("int a(){return 1;}\n"), 0o644)
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("changed\n"), 0o644)

	changed := ChangedSince(root, base)
	if len(changed) != 1 || filepath.Base(changed[0]) != "a.c" {
		t.Errorf("ChangedSince = %v, want [a.c] (non-source filtered out)", changed)
	}
}

func TestEmptyRevAndNonRepo(t *testing.T) {
	if c := ChangedSince(t.TempDir(), ""); c != nil {
		t.Errorf("empty rev should yield nil, got %v", c)
	}
	if h := Head(t.TempDir()); h != "" {
		t.Errorf("non-repo Head should be empty, got %q", h)
	}
}
