// Package csrc holds the shared, pure-text C/C++ source scanning primitives used
// by the build-less heuristics (internal/fnptr's dispatch synthesizer and
// internal/cindex's definition index). These never invoke clangd and never
// evaluate the preprocessor, so they see code inside every `#ifdef` branch
// regardless of which config is active — the deliberate complement to clangd's
// config-accurate (but config-gated) view.
package csrc

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/swchen44/ccq/internal/config"
)

// Files returns the C/C++ source/header files under root, applying the project's
// ccq.json allow/deny filter and skipping vendored/build directories.
func Files(root string) []string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				b := filepath.Base(p)
				if b == ".git" || b == "build" || b == "node_modules" || b == ".cache" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp":
			if config.Keep(p) {
				out = append(out, p)
			}
		}
		return nil
	})
	return out
}

// ReadLines reads a file and splits it into lines (nil on error).
func ReadLines(f string) []string {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// StripCodeLine removes a single line's `//` and single-line `/* */` comments and
// blanks the interior of string/char literals (keeping the delimiters), so a
// `p->fn()` token inside a string is not mistaken for real code.
func StripCodeLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			break // line comment: drop the rest
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' { // block comment (single line)
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 3
				continue
			}
			break
		}
		if c == '"' || c == '\'' { // keep the delimiters, blank the interior
			b.WriteByte(c)
			i++
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == c {
					b.WriteByte(c)
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// StripComments removes // and (multi-line) /* */ comments from a whole block,
// preserving newlines so line numbers stay aligned.
func StripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			j := strings.Index(s[i+2:], "*/")
			seg := s[i:]
			if j >= 0 {
				seg = s[i : i+2+j+2]
			}
			for _, r := range seg { // keep the comment's newlines
				if r == '\n' {
					b.WriteByte('\n')
				}
			}
			if j < 0 {
				break
			}
			i += 2 + j + 2
			continue
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
