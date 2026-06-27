// Package cmd implements the ccq subcommands on top of an lsp.Client.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/swchen44/ccq/internal/fnptr"
	"github.com/swchen44/ccq/internal/lsp"
)

// Ctx carries the shared state for a command run.
type Ctx struct {
	Client *lsp.Client
	Root   string
	JSON   bool
	Out    io.Writer // where command output goes (stdout, or a daemon response buffer)
}

// Request is a command to run (used by direct mode and the daemon).
type Request struct {
	Cmd   string   `json:"cmd"`
	Args  []string `json:"args"`
	JSON  bool     `json:"json"`
	Depth int      `json:"depth"`
	Apply bool     `json:"apply"`
}

// Dispatch runs a single command. Output goes to c.Out. Returns false for an
// unknown command.
func (c *Ctx) Dispatch(r Request) bool {
	c.JSON = r.JSON
	a := func(i int) string {
		if i < len(r.Args) {
			return r.Args[i]
		}
		return ""
	}
	switch r.Cmd {
	case "search":
		c.Search(a(0))
	case "def", "show":
		c.Def(a(0))
	case "refs", "usages":
		c.Refs(a(0))
	case "callers":
		c.Callers(a(0))
	case "callees":
		c.Callees(a(0))
	case "impact":
		c.Impact(a(0), r.Depth)
	case "explore":
		c.Explore(a(0))
	case "symbols":
		c.Symbols(a(0))
	case "macro":
		c.Macro(a(0))
	case "rename":
		c.Rename(a(0), a(1), r.Apply)
	default:
		return false
	}
	return true
}

// resolveSymbol maps a bare symbol name to a definition location.
// Retries because clangd's background index may not be ready on the first call.
func (c *Ctx) resolveSymbol(name string) (file string, pos lsp.Position, ok bool) {
	var syms []lsp.SymbolInfo
	for try := 0; try < 6; try++ {
		syms, _ = c.Client.WorkspaceSymbol(name)
		if len(syms) > 0 {
			break
		}
		time.Sleep(1500 * time.Millisecond)
	}
	// prefer exact name match; prefer function kinds (12=Function,6=Method)
	var best *lsp.SymbolInfo
	for i := range syms {
		if syms[i].Name != name {
			continue
		}
		if best == nil || isFuncKind(syms[i].Kind) && !isFuncKind(best.Kind) {
			best = &syms[i]
		}
	}
	if best == nil && len(syms) > 0 {
		best = &syms[0]
	}
	if best == nil {
		return "", lsp.Position{}, false
	}
	f := lsp.URIToPath(best.Location.URI)
	p := best.Location.Range.Start
	// clangd's workspace/symbol range may start at the declaration (e.g. the
	// return type), not the name. call hierarchy / definition need the cursor on
	// the name — locate the name column on that line.
	if col := nameColumn(f, p.Line, name); col >= 0 {
		p.Character = col
	}
	return f, p, true
}

// nameColumn finds the 0-based column of name on the given line, or -1.
func nameColumn(file string, line int, name string) int {
	txt := lsp.LineText0(file, line)
	idx := strings.Index(txt, name)
	for idx >= 0 {
		before := idx == 0 || !isIdentChar(rune(txt[idx-1]))
		after := idx+len(name) >= len(txt) || !isIdentChar(rune(txt[idx+len(name)]))
		if before && after {
			return idx
		}
		next := strings.Index(txt[idx+1:], name)
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return -1
}

func isIdentChar(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func isFuncKind(k int) bool { return k == 12 || k == 6 }

// Search: workspace/symbol.
func (c *Ctx) Search(query string) {
	syms, _ := c.Client.WorkspaceSymbol(query)
	if c.JSON {
		c.emit(syms)
		return
	}
	for _, s := range syms {
		fmt.Fprintf(c.Out, "%s\t%s:%d\t[%s]\n", s.Name, lsp.URIToPath(s.Location.URI),
			s.Location.Range.Start.Line+1, kindName(s.Kind))
	}
}

// Def: show the definition snippet of a symbol.
func (c *Ctx) Def(name string) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	locs, _ := c.Client.Definition(file, pos)
	if len(locs) == 0 {
		fmt.Fprintf(c.Out, "%s\t%s:%d\n", name, file, pos.Line+1)
		return
	}
	l := locs[0]
	f := lsp.URIToPath(l.URI)
	fmt.Fprintf(c.Out, "// %s:%d\n%s\n", f, l.Range.Start.Line+1,
		lsp.Snippet(f, l.Range.Start.Line, l.Range.End.Line))
}

// Refs: all references.
func (c *Ctx) Refs(name string) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	locs, _ := c.Client.References(file, pos, false)
	if c.JSON {
		c.emit(locs)
		return
	}
	for _, l := range locs {
		f := lsp.URIToPath(l.URI)
		fmt.Fprintf(c.Out, "%s:%d\t%s\n", f, l.Range.Start.Line+1, lsp.LineText(f, l.Range.Start.Line))
	}
}

// callerNames returns function-level callers via clangd, plus fnptr-heuristic ones.
func (c *Ctx) callerNames(name string) (real []string, heuristic []fnptr.Caller) {
	file, pos, ok := c.resolveSymbol(name)
	if ok {
		items, _ := c.Client.PrepareCallHierarchy(file, pos)
		if os.Getenv("CCQ_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[debug] resolve %s -> %s:%d:%d, prepareCallHierarchy items=%d\n",
				name, file, pos.Line+1, pos.Character, len(items))
		}
		if len(items) > 0 {
			callers, _ := c.Client.IncomingCalls(items[0])
			if os.Getenv("CCQ_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[debug] incomingCalls=%d\n", len(callers))
			}
			for _, x := range callers {
				real = append(real, x.Name)
			}
		}
	}
	heuristic = fnptr.Callers(c.Root, name)
	return dedup(real), heuristic
}

// Callers: who calls this (clangd + fnptr heuristic).
func (c *Ctx) Callers(name string) {
	real, heur := c.callerNames(name)
	if c.JSON {
		c.emit(map[string]any{"symbol": name, "callers": real, "fnptr_heuristic": heur})
		return
	}
	fmt.Fprintf(c.Out, "callers of %s:\n", name)
	for _, r := range real {
		fmt.Fprintf(c.Out, "  %s\n", r)
	}
	for _, h := range heur {
		fmt.Fprintf(c.Out, "  %s  (fnptr via .%s @ %s:%d)\n", h.Func, h.Field, h.File, h.Line)
	}
	if len(real)+len(heur) == 0 {
		fmt.Fprintln(c.Out, "  (none)")
	}
}

// Callees: what this calls.
func (c *Ctx) Callees(name string) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	items, _ := c.Client.PrepareCallHierarchy(file, pos)
	var out []string
	if len(items) > 0 {
		callees, _ := c.Client.OutgoingCalls(items[0])
		for _, x := range callees {
			out = append(out, x.Name)
		}
	}
	out = dedup(out)
	if c.JSON {
		c.emit(map[string]any{"symbol": name, "callees": out})
		return
	}
	fmt.Fprintf(c.Out, "callees of %s:\n", name)
	for _, o := range out {
		fmt.Fprintf(c.Out, "  %s\n", o)
	}
}

// Impact: transitive callers up to depth.
func (c *Ctx) Impact(name string, depth int) {
	visited := map[string]bool{name: true}
	frontier := []string{name}
	var order []string
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, sym := range frontier {
			file, pos, ok := c.resolveSymbol(sym)
			if !ok {
				continue
			}
			items, _ := c.Client.PrepareCallHierarchy(file, pos)
			if len(items) == 0 {
				continue
			}
			callers, _ := c.Client.IncomingCalls(items[0])
			for _, x := range callers {
				if !visited[x.Name] {
					visited[x.Name] = true
					next = append(next, x.Name)
					order = append(order, fmt.Sprintf("%s (hop %d)", x.Name, d+1))
				}
			}
		}
		frontier = next
	}
	if c.JSON {
		c.emit(map[string]any{"symbol": name, "depth": depth, "impacted": order})
		return
	}
	fmt.Fprintf(c.Out, "impact radius of %s (depth %d): %d symbols\n", name, depth, len(order))
	for _, o := range order {
		fmt.Fprintf(c.Out, "  %s\n", o)
	}
}

// Explore: one-shot source + callers + callees + blast radius (CodeGraph-style).
func (c *Ctx) Explore(name string) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	locs, _ := c.Client.Definition(file, pos)
	real, heur := c.callerNames(name)
	var callees []string
	if items, _ := c.Client.PrepareCallHierarchy(file, pos); len(items) > 0 {
		cs, _ := c.Client.OutgoingCalls(items[0])
		for _, x := range cs {
			callees = append(callees, x.Name)
		}
	}
	callees = dedup(callees)
	var heurNames []string
	for _, h := range heur {
		heurNames = append(heurNames, h.Func+" (fnptr)")
	}
	if c.JSON {
		c.emit(map[string]any{"symbol": name, "callers": real, "fnptr_callers": heurNames,
			"callees": callees, "blast_radius": len(real) + len(heur)})
		return
	}
	fmt.Fprintf(c.Out, "=== explore: %s ===\n", name)
	if len(locs) > 0 {
		l := locs[0]
		f := lsp.URIToPath(l.URI)
		fmt.Fprintf(c.Out, "--- source (%s:%d) ---\n%s\n", f, l.Range.Start.Line+1,
			lsp.Snippet(f, l.Range.Start.Line, l.Range.End.Line))
	}
	fmt.Fprintf(c.Out, "--- callers (%d) ---\n  %s\n", len(real)+len(heur),
		strings.Join(append(real, heurNames...), "\n  "))
	fmt.Fprintf(c.Out, "--- callees (%d) ---\n  %s\n", len(callees), strings.Join(callees, "\n  "))
}

// Symbols: file outline.
func (c *Ctx) Symbols(file string) {
	res, _ := c.Client.DocumentSymbol(file)
	if c.JSON {
		fmt.Fprintln(c.Out, string(res))
		return
	}
	var syms []struct {
		Name string `json:"name"`
		Kind int    `json:"kind"`
		Range lsp.Range `json:"range"`
	}
	json.Unmarshal(res, &syms)
	for _, s := range syms {
		fmt.Fprintf(c.Out, "%s\t[%s]\tL%d\n", s.Name, kindName(s.Kind), s.Range.Start.Line+1)
	}
}

// Macro: hover (clangd expands macros / shows signatures).
func (c *Ctx) Macro(name string) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	res, _ := c.Client.Hover(file, pos)
	var h struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	if json.Unmarshal(res, &h) == nil && h.Contents.Value != "" {
		fmt.Fprintln(c.Out, h.Contents.Value)
	} else {
		fmt.Fprintln(c.Out, string(res))
	}
}

// Rename: workspace-wide safe rename (Serena-parity editing).
func (c *Ctx) Rename(name, newName string, apply bool) {
	file, pos, ok := c.resolveSymbol(name)
	if !ok {
		fmt.Fprintf(c.Out, "symbol not found: %s\n", name)
		return
	}
	res, _ := c.Client.Rename(file, pos, newName)
	edits := parseWorkspaceEdit(res)
	if c.JSON {
		c.emit(map[string]any{"symbol": name, "newName": newName, "edits": edits})
		return
	}
	fmt.Fprintf(c.Out, "rename %s -> %s : %d edits across %d files\n", name, newName,
		countEdits(edits), len(edits))
	for f, es := range edits {
		fmt.Fprintf(c.Out, "  %s (%d)\n", f, len(es))
	}
	if apply {
		n, err := applyEdits(edits)
		if err != nil {
			fmt.Fprintf(c.Out, "apply failed: %v\n", err)
			return
		}
		fmt.Fprintf(c.Out, "applied %d edits.\n", n)
	} else {
		fmt.Fprintln(c.Out, "(dry-run; pass --apply to write changes)")
	}
}

func (c *Ctx) emit(v any) { b, _ := json.MarshalIndent(v, "", "  "); fmt.Fprintln(c.Out, string(b)) }

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// IndexWaits returns (max, baseline) for clangd index settling. WaitIndex
// returns as soon as $/progress reports indexing done, else after baseline.
func IndexWaits(big bool) (max, baseline time.Duration) {
	if big {
		return 90 * time.Second, 20 * time.Second
	}
	return 30 * time.Second, 6 * time.Second
}
