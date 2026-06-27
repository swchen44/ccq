package cmd

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/swchen44/ccq/internal/lsp"
)

type textEdit struct {
	Range   lsp.Range `json:"range"`
	NewText string    `json:"newText"`
}

// parseWorkspaceEdit handles both `changes` and `documentChanges` shapes.
func parseWorkspaceEdit(res json.RawMessage) map[string][]textEdit {
	out := map[string][]textEdit{}
	var we struct {
		Changes         map[string][]textEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []textEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if json.Unmarshal(res, &we) != nil {
		return out
	}
	for uri, edits := range we.Changes {
		out[lsp.URIToPath(uri)] = edits
	}
	for _, dc := range we.DocumentChanges {
		out[lsp.URIToPath(dc.TextDocument.URI)] = append(out[lsp.URIToPath(dc.TextDocument.URI)], dc.Edits...)
	}
	return out
}

func countEdits(m map[string][]textEdit) int {
	n := 0
	for _, e := range m {
		n += len(e)
	}
	return n
}

// applyEdits applies text edits to files on disk (bottom-up to keep offsets valid).
func applyEdits(edits map[string][]textEdit) (int, error) {
	total := 0
	for file, es := range edits {
		b, err := os.ReadFile(file)
		if err != nil {
			return total, err
		}
		lines := strings.Split(string(b), "\n")
		// sort edits bottom-up, right-to-left
		sort.Slice(es, func(i, j int) bool {
			if es[i].Range.Start.Line != es[j].Range.Start.Line {
				return es[i].Range.Start.Line > es[j].Range.Start.Line
			}
			return es[i].Range.Start.Character > es[j].Range.Start.Character
		})
		for _, e := range es {
			lines = applyOne(lines, e)
			total++
		}
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return total, err
		}
	}
	return total, nil
}

func applyOne(lines []string, e textEdit) []string {
	sl, sc := e.Range.Start.Line, e.Range.Start.Character
	el, ec := e.Range.End.Line, e.Range.End.Character
	if sl < 0 || sl >= len(lines) || el >= len(lines) {
		return lines
	}
	if sl == el {
		ln := lines[sl]
		if sc <= len(ln) && ec <= len(ln) {
			lines[sl] = ln[:sc] + e.NewText + ln[ec:]
		}
		return lines
	}
	// multi-line replace
	head := lines[sl][:min(sc, len(lines[sl]))]
	tail := ""
	if ec <= len(lines[el]) {
		tail = lines[el][ec:]
	}
	merged := head + e.NewText + tail
	out := append([]string{}, lines[:sl]...)
	out = append(out, merged)
	out = append(out, lines[el+1:]...)
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func kindName(k int) string {
	names := map[int]string{
		1: "file", 2: "module", 5: "class", 6: "method", 7: "property",
		8: "field", 9: "constructor", 10: "enum", 11: "interface", 12: "function",
		13: "variable", 14: "constant", 15: "macro", // clangd maps C/C++ macros to kind 15
		22: "struct", 23: "event", 26: "typeparam",
	}
	if n, ok := names[k]; ok {
		return n
	}
	return "sym"
}
