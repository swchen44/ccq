// Package compdb locates or generates a compile_commands.json for a project,
// which clangd needs for accurate cross-file / #ifdef / macro resolution.
package compdb

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Locate returns the directory containing a compile_commands.json, searching
// root and common build dirs. Returns "" if none found.
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
	return ""
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
	return "", "", fmt.Errorf("no compile_commands.json found and could not generate one (need CMake/Meson, or bear+make). " +
		"Generate manually: `bear -- make` or `cmake -DCMAKE_EXPORT_COMPILE_COMMANDS=ON -B build`")
}

func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }
func have(bin string) bool     { _, e := exec.LookPath(bin); return e == nil }
