package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/swchen44/ccq/internal/fnptr"
	"github.com/swchen44/ccq/internal/lsp"
)

type exNode struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}
type exEdge struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Kind string `json:"kind"` // "calls" | "fnptr"
}

// Export dumps the project's symbols + call graph as JSON or a SQLite-loadable
// SQL script — a zero-dependency substitute for an in-tool query language
// (run `ccq export --format sql | sqlite3 g.db` then query with plain SQL).
func (c *Ctx) Export(format, outPath string) {
	var nodes []exNode
	var edges []exEdge
	seenNode := map[string]bool{}
	seenEdge := map[string]bool{}
	funcFiles := map[string]string{} // function name -> file (for call hierarchy)
	funcPos := map[string]lsp.Position{}

	// 1) nodes via documentSymbol over every source file
	for _, f := range sourceFiles(c.Root) {
		res, err := c.Client.DocumentSymbol(f)
		if err != nil || res == nil {
			continue
		}
		// clangd returns flat SymbolInformation[]: the range lives in location.range.
		var syms []struct {
			Name     string       `json:"name"`
			Kind     int          `json:"kind"`
			Location lsp.Location `json:"location"`
		}
		json.Unmarshal(res, &syms)
		for _, s := range syms {
			key := s.Name + "|" + f
			if seenNode[key] {
				continue
			}
			seenNode[key] = true
			line := s.Location.Range.Start.Line
			nodes = append(nodes, exNode{s.Name, kindName(s.Kind), f, line + 1})
			if isFuncKind(s.Kind) {
				funcFiles[s.Name] = f
				// position the cursor on the name for call hierarchy.
				pos := s.Location.Range.Start
				if col := nameColumn(f, line, s.Name); col >= 0 {
					pos.Character = col
				}
				funcPos[s.Name] = pos
			}
		}
	}

	// 2) call edges via incomingCalls for each function (clangd's outgoingCalls
	// is unreliable; incomingCalls is solid). For each function F, every caller
	// X yields an edge X->F — the same call graph, built from the caller side.
	// Use resolveSymbol for the cursor (same proven path as `ccq callers`).
	_ = funcFiles
	_ = funcPos
	for name := range funcFiles {
		file, pos, ok := c.resolveSymbol(name)
		if !ok {
			continue
		}
		items, _ := c.Client.PrepareCallHierarchy(file, pos)
		if len(items) == 0 {
			continue
		}
		callers, _ := c.Client.IncomingCalls(items[0])
		for _, x := range callers {
			ek := x.Name + ">" + name + "|calls"
			if !seenEdge[ek] {
				seenEdge[ek] = true
				edges = append(edges, exEdge{x.Name, name, "calls"})
			}
		}
	}

	// 3) fnptr heuristic edges (dispatcher -> handler), once per handler node
	for _, n := range nodes {
		if n.Kind != "function" {
			continue
		}
		for _, h := range fnptr.Callers(c.Root, n.Name) {
			ek := h.Func + ">" + n.Name + "|fnptr"
			if !seenEdge[ek] {
				seenEdge[ek] = true
				edges = append(edges, exEdge{h.Func, n.Name, "fnptr"})
			}
		}
	}

	// Default: write the dump to c.Out (stdout in direct mode, or the daemon's
	// response back to the client). --out writes to a file instead.
	var out io.Writer = c.Out
	var fh *os.File
	if outPath != "" {
		if f, err := os.Create(outPath); err == nil {
			fh = f
			defer fh.Close()
			out = fh
		}
	}

	if format == "sql" {
		writeSQL(out, nodes, edges)
	} else {
		b, _ := json.MarshalIndent(map[string]any{"nodes": nodes, "edges": edges}, "", " ")
		fmt.Fprintln(out, string(b))
	}
	if outPath != "" {
		fmt.Fprintf(c.Out, "exported %d nodes, %d edges -> %s\n", len(nodes), len(edges), outPath)
	}
}

func writeSQL(out io.Writer, nodes []exNode, edges []exEdge) {
	fmt.Fprintln(out, "BEGIN;")
	fmt.Fprintln(out, "CREATE TABLE IF NOT EXISTS nodes(name TEXT, kind TEXT, file TEXT, line INT);")
	fmt.Fprintln(out, "CREATE TABLE IF NOT EXISTS edges(src TEXT, dst TEXT, kind TEXT);")
	for _, n := range nodes {
		fmt.Fprintf(out, "INSERT INTO nodes VALUES('%s','%s','%s',%d);\n",
			sqlEsc(n.Name), n.Kind, sqlEsc(n.File), n.Line)
	}
	for _, e := range edges {
		fmt.Fprintf(out, "INSERT INTO edges VALUES('%s','%s','%s');\n",
			sqlEsc(e.Src), sqlEsc(e.Dst), e.Kind)
	}
	fmt.Fprintln(out, "COMMIT;")
}

func sqlEsc(s string) string { return strings.ReplaceAll(s, "'", "''") }

func sourceFiles(root string) []string {
	var out []string
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
		case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp":
			out = append(out, p)
		}
		return nil
	})
	return out
}
