package lsp

import (
	"os"
	"strings"
)

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func isCpp(file string) bool {
	for _, ext := range []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx", ".c++"} {
		if strings.HasSuffix(file, ext) {
			return true
		}
	}
	return false
}

// LineText returns the 0-based line from a file (best effort).
func LineText(file string, line int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line >= 0 && line < len(lines) {
		return strings.TrimSpace(lines[line])
	}
	return ""
}

// LineText0 returns the raw (un-trimmed) 0-based line — for column math.
func LineText0(file string, line int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line >= 0 && line < len(lines) {
		return lines[line]
	}
	return ""
}

// Snippet returns the text of [start,end] lines from a file.
func Snippet(file string, start, end int) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if start < 0 {
		start = 0
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}
