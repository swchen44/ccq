// Package cache inspects and cleans ccq's on-disk caches: the per-project daemon
// state (UserCacheDir/ccq/<hash>), the staged compile databases
// (UserCacheDir/ccq/compdb/<hash>), and each project's clangd index
// (<root>/.cache/clangd). It backs `ccq cache list/clean/path`.
//
// NOTE: <root>/.cache/clangd is clangd's default index location and is SHARED
// with editor clangd (VS Code, etc.). Removing it makes those re-index too.
package cache

import (
	"os"
	"path/filepath"
	"time"

	"github.com/swchen44/ccq/internal/daemon"
)

// Entry is one cache item shown by `ccq cache`.
type Entry struct {
	Kind     string    // "daemon" | "compdb" | "clangd-index"
	Dir      string    // the directory on disk
	Project  string    // project root (daemon/clangd-index); "" for compdb
	Mode     string    // index mode (daemon)
	Files    int       // files indexed (daemon)
	Size     int64     // bytes on disk
	Modified time.Time // newest mtime under Dir
	Running  bool      // daemon currently up
}

// Base returns ccq's cache root (UserCacheDir/ccq).
func Base() string { return daemon.CacheBase() }

// List enumerates all ccq + clangd caches.
func List() []Entry {
	var out []Entry
	base := Base()
	roots := map[string]bool{} // unique project roots (for clangd-index entries)

	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if e.Name() == "compdb" {
			// staged compile DBs, one subdir per --compdb set
			subs, _ := os.ReadDir(dir)
			for _, s := range subs {
				if s.IsDir() {
					sd := filepath.Join(dir, s.Name())
					sz, mt := dirStat(sd)
					out = append(out, Entry{Kind: "compdb", Dir: sd, Size: sz, Modified: mt})
				}
			}
			continue
		}
		// a per-project daemon state dir
		m, ok := daemon.ReadMeta(dir)
		sz, mt := dirStat(dir)
		ent := Entry{Kind: "daemon", Dir: dir, Size: sz, Modified: mt}
		if ok {
			ent.Project, ent.Mode, ent.Files = m.Root, m.Mode, m.Files
			roots[m.Root] = true
			if _, err := daemon.Status(m.Root); err == nil {
				ent.Running = true
			}
		}
		out = append(out, ent)
	}
	// clangd index per known project (.cache/clangd) — the big one, shared with editors
	for r := range roots {
		cd := filepath.Join(r, ".cache", "clangd")
		if sz, mt := dirStat(cd); sz > 0 {
			out = append(out, Entry{Kind: "clangd-index", Dir: cd, Project: r, Size: sz, Modified: mt})
		}
	}
	return out
}

// CleanOpts selects what Clean removes.
type CleanOpts struct {
	All       bool
	Project   string        // only this project root
	OlderThan time.Duration // only entries not modified within this window (0 = no age filter)
	Index     bool          // also remove the clangd .cache/clangd (shared with editors!)
}

// Clean removes the selected caches. Returns the entries it removed. Daemons are
// shut down before their state is deleted.
func Clean(opts CleanOpts) []Entry {
	var removed []Entry
	for _, e := range List() {
		if !match(e, opts) {
			continue
		}
		if e.Kind == "clangd-index" && !opts.Index {
			continue // only touch clangd's index when explicitly asked
		}
		if e.Kind == "daemon" && e.Project != "" {
			daemon.Shutdown(e.Project)
		}
		if os.RemoveAll(e.Dir) == nil {
			removed = append(removed, e)
		}
	}
	return removed
}

func match(e Entry, o CleanOpts) bool {
	if o.Project != "" {
		return e.Project == o.Project
	}
	if o.OlderThan > 0 {
		return time.Since(e.Modified) >= o.OlderThan
	}
	return o.All
}

// dirStat returns the total size and newest mtime under dir.
func dirStat(dir string) (int64, time.Time) {
	var size int64
	var newest time.Time
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return size, newest
}
