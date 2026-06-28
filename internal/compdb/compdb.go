// Package compdb locates or generates a compile_commands.json for a project,
// which clangd needs for accurate cross-file / #ifdef / macro resolution.
package compdb

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/swchen44/ccq/internal/config"
)

// Stage merges one or more compile_commands.json files (any filenames, anywhere)
// into a single compile_commands.json inside a stable per-input cache dir, and
// returns that dir for clangd's --compile-commands-dir. Use it for projects whose
// build emits several differently-named databases (e.g. one per executable):
//
//	ccq callers foo --compdb build1.json,build2.json,build3.json
//
// Entries are concatenated — a file built several ways keeps all its entries and
// clangd picks one per file (so #ifdef/-D reflects one of the configs; query a
// single --compdb for an exact per-config view). The cache dir is stable per input
// set so clangd's on-disk index persists across runs.
func Stage(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	var merged []json.RawMessage
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("--compdb %s: %w", p, err)
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err != nil {
			return "", fmt.Errorf("--compdb %s: not a compile_commands.json array: %w", p, err)
		}
		merged = append(merged, arr...)
	}
	h := sha1.Sum([]byte(strings.Join(paths, "\x00")))
	base, _ := os.UserCacheDir()
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "ccq", "compdb", hex.EncodeToString(h[:8]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "compile_commands.json"), out, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// Locate returns the directory clangd should use as its compile-commands-dir:
// one containing a compile_commands.json, or (no-build fallback) a
// compile_flags.txt. Returns "" if neither is found.
func Locate(root string) string {
	candidates := []string{
		root,
		filepath.Join(root, "build"),
		filepath.Join(root, "out"),
		filepath.Join(root, "cmake-build-debug"),
		filepath.Join(root, "builddir"), // meson
	}
	for _, d := range candidates {
		if fileExists(filepath.Join(d, "compile_commands.json")) {
			return d
		}
	}
	if fileExists(filepath.Join(root, "compile_flags.txt")) {
		return root
	}
	return ""
}

// IsNoBuild reports whether the located config is the lightweight no-build
// compile_flags.txt (vs a full compile_commands.json).
func IsNoBuild(root string) bool {
	if Locate(root) == root && fileExists(filepath.Join(root, "compile_flags.txt")) {
		// compile_commands at root wins if present
		return !fileExists(filepath.Join(root, "compile_commands.json"))
	}
	return false
}

// Ensure returns a directory with compile_commands.json, generating one if
// possible. Strategy: existing > CMake > Meson > bear+make. Returns dir + how.
func Ensure(root string) (dir string, how string, err error) {
	if d := Locate(root); d != "" {
		return d, "existing", nil
	}
	// CMake
	if fileExists(filepath.Join(root, "CMakeLists.txt")) && have("cmake") {
		bd := filepath.Join(root, "build")
		os.MkdirAll(bd, 0o755)
		cmd := exec.Command("cmake", "-DCMAKE_EXPORT_COMPILE_COMMANDS=ON", "-S", root, "-B", bd)
		if out, e := cmd.CombinedOutput(); e == nil && fileExists(filepath.Join(bd, "compile_commands.json")) {
			return bd, "cmake", nil
		} else if e != nil {
			_ = out
		}
	}
	// Meson
	if fileExists(filepath.Join(root, "meson.build")) && have("meson") {
		bd := filepath.Join(root, "builddir")
		if _, e := exec.Command("meson", "setup", bd, root).CombinedOutput(); e == nil &&
			fileExists(filepath.Join(bd, "compile_commands.json")) {
			return bd, "meson", nil
		}
	}
	// bear + make
	if fileExists(filepath.Join(root, "Makefile")) && have("bear") && have("make") {
		cmd := exec.Command("bear", "--", "make", "-j4")
		cmd.Dir = root
		if _, e := cmd.CombinedOutput(); e == nil && fileExists(filepath.Join(root, "compile_commands.json")) {
			return root, "bear+make", nil
		}
	}
	// No-build fallback (cbm-style breadth): generate compile_flags.txt with
	// auto-discovered include dirs. clangd then works cross-file (with ccq's
	// OpenAll) WITHOUT a build — at lower accuracy (#ifdef over-included, no -D).
	if err := writeCompileFlags(root); err == nil {
		return root, "compile_flags(no-build)", nil
	}
	return "", "", fmt.Errorf("no compile_commands.json found and could not generate one (need CMake/Meson, or bear+make). " +
		"Generate manually: `bear -- make` or `cmake -DCMAKE_EXPORT_COMPILE_COMMANDS=ON -B build`")
}

// writeCompileFlags creates a compile_flags.txt with -I for every directory
// that contains a header, plus a C standard. This is the no-build mode.
func writeCompileFlags(root string) error {
	incl := map[string]bool{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				b := filepath.Base(p)
				if b == ".git" || b == "build" || b == ".cache" || b == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".h", ".hpp", ".hh", ".hxx":
			if config.Keep(p) {
				incl[filepath.Dir(p)] = true
			}
		}
		return nil
	})
	var lines []string
	lines = append(lines, "-xc", "-std=gnu11")
	for d := range incl {
		rel, err := filepath.Rel(root, d)
		if err != nil {
			rel = d
		}
		lines = append(lines, "-I"+rel)
	}
	return os.WriteFile(filepath.Join(root, "compile_flags.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func have(bin string) bool     { _, e := exec.LookPath(bin); return e == nil }
