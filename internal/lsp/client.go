// Package lsp is a minimal LSP client that drives clangd over stdio (JSON-RPC).
// It exposes the subset of methods ccq needs: symbols, definition, references,
// call hierarchy, document symbols, hover, rename.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/swchen44/ccq/internal/config"
)

// Client wraps a clangd subprocess speaking LSP over stdio.
type Client struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	root     string
	mu       sync.Mutex
	nextID   int
	pending  map[int]chan json.RawMessage
	opened   map[string]bool
	ver      map[string]int
	idxMu    sync.Mutex
	idxBegan bool
	idxEnded bool
}

// Position is a 0-based line/character.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span in a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a uri + range.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// SymbolInfo is a workspace/symbol result item.
type SymbolInfo struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
	Detail   string   `json:"containerName"`
}

// CallHierarchyItem identifies a function for call-hierarchy queries.
// Data is clangd's opaque payload and MUST be round-tripped verbatim, or
// incomingCalls/outgoingCalls silently return nothing.
type CallHierarchyItem struct {
	Name           string          `json:"name"`
	Kind           int             `json:"kind"`
	URI            string          `json:"uri"`
	Range          Range           `json:"range"`
	SelectionRange Range           `json:"selectionRange"`
	Detail         string          `json:"detail,omitempty"`
	Tags           json.RawMessage `json:"tags,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

// Start launches clangd rooted at root, pointing it at the compile_commands.json dir.
func Start(clangdBin, root, compileCommandsDir string) (*Client, error) {
	if clangdBin == "" {
		clangdBin = "clangd"
	}
	args := []string{"--background-index", "--log=error"}
	if compileCommandsDir != "" {
		args = append(args, "--compile-commands-dir="+compileCommandsDir)
	}
	cmd := exec.Command(clangdBin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start clangd (%s): %w", clangdBin, err)
	}
	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		root:    root,
		pending: map[int]chan json.RawMessage{},
		opened:  map[string]bool{},
		ver:     map[string]int{},
	}
	go c.readLoop()
	if err := c.initialize(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) readLoop() {
	for {
		// read headers
		var length int
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				fmt.Sscanf(strings.TrimSpace(line[len("content-length:"):]), "%d", &length)
			}
		}
		if length == 0 {
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if msg.ID == nil {
			if msg.Method == "$/progress" {
				c.trackProgress(msg.Params)
			}
			continue // other notifications ignored
		}
		c.mu.Lock()
		ch := c.pending[*msg.ID]
		delete(c.pending, *msg.ID)
		c.mu.Unlock()
		if ch != nil {
			if msg.Error != nil {
				ch <- nil
			} else {
				ch <- msg.Result
			}
		}
	}
}

func (c *Client) send(method string, params any, notify bool) (json.RawMessage, error) {
	c.mu.Lock()
	msg := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	var ch chan json.RawMessage
	var id int
	if !notify {
		c.nextID++
		id = c.nextID
		msg["id"] = id
		ch = make(chan json.RawMessage, 1)
		c.pending[id] = ch
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(data))
	c.stdin.Write(data)
	c.mu.Unlock()
	if notify {
		return nil, nil
	}
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(60 * time.Second):
		return nil, fmt.Errorf("lsp timeout on %s", method)
	}
}

func (c *Client) initialize() error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   pathToURI(c.root),
		"capabilities": map[string]any{
			"window": map[string]any{"workDoneProgress": true},
			"textDocument": map[string]any{
				"callHierarchy": map[string]any{"dynamicRegistration": false},
				"rename":        map[string]any{"dynamicRegistration": false},
			},
		},
	}
	if _, err := c.send("initialize", params, false); err != nil {
		return err
	}
	c.send("initialized", map[string]any{}, true)
	return nil
}

// trackProgress watches clangd's $/progress to know when background indexing ends.
func (c *Client) trackProgress(params json.RawMessage) {
	var p struct {
		Value struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"value"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.idxMu.Lock()
	defer c.idxMu.Unlock()
	switch p.Value.Kind {
	case "begin":
		if strings.Contains(strings.ToLower(p.Value.Title), "index") {
			c.idxBegan = true
		}
	case "end":
		if c.idxBegan {
			c.idxEnded = true
		}
	}
}

// WaitIndex waits for clangd's background indexing to complete (via $/progress),
// up to max. If clangd never reports indexing progress (some versions don't),
// it falls back to waiting `baseline` so cross-file call hierarchy can settle.
func (c *Client) WaitIndex(max, baseline time.Duration) {
	start := time.Now()
	deadline := start.Add(max)
	for time.Now().Before(deadline) {
		c.idxMu.Lock()
		began, ended := c.idxBegan, c.idxEnded
		c.idxMu.Unlock()
		if began && ended {
			return // indexing finished — fastest correct path
		}
		if !began && time.Since(start) >= baseline {
			return // no progress reported; baseline elapsed — proceed
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// OpenAll opens every C/C++ source file under root (up to cap) so clangd's
// dynamic index can answer workspace/symbol and cross-file call hierarchy.
// clangd's background static index alone does not reliably populate these on a
// cold project, so we prime it by opening files. Returns the number opened.
func (c *Client) OpenAll(root string, cap int) int {
	return c.openAllAfter(root, cap, nil)
}

// OpenFiles opens a specific list of files (idempotent), returning how many were
// newly opened. Used to prioritise changed files on a warm restart.
func (c *Client) OpenFiles(files []string) int {
	n := 0
	for _, f := range files {
		if c.Open(f) == nil {
			n++
		}
	}
	return n
}

// openAllAfter opens priority files first, then walks the tree for the rest.
func (c *Client) openAllAfter(root string, cap int, priority []string) int {
	n := c.OpenFiles(priority)
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
		if n >= cap {
			return filepath.SkipAll
		}
		switch filepath.Ext(p) {
		case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hh", ".hxx":
			if config.Keep(p) && c.Open(p) == nil {
				n++
			}
		}
		return nil
	})
	return n
}

// Open notifies clangd of a file (required before position-based queries).
func (c *Client) Open(file string) error {
	if c.opened[file] {
		return nil
	}
	text, err := readFile(file)
	if err != nil {
		return err
	}
	lang := "c"
	if isCpp(file) {
		lang = "cpp"
	}
	c.send("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": pathToURI(file), "languageId": lang, "version": 1, "text": text,
		},
	}, true)
	c.opened[file] = true
	c.ver[file] = 1
	return nil
}

// Reload re-syncs clangd with the on-disk content of file after an external edit
// (rename/replace-body/insert --apply). Without this, a warm daemon keeps serving
// the pre-edit index. If the file was open, send a full-text didChange with a bumped
// version; otherwise open it fresh.
func (c *Client) Reload(file string) error {
	if !c.opened[file] {
		return c.Open(file)
	}
	text, err := readFile(file)
	if err != nil {
		return err
	}
	c.ver[file]++
	c.send("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": pathToURI(file), "version": c.ver[file]},
		"contentChanges": []map[string]any{{"text": text}},
	}, true)
	return nil
}

// WorkspaceSymbol searches symbols by name across the project.
func (c *Client) WorkspaceSymbol(query string) ([]SymbolInfo, error) {
	res, err := c.send("workspace/symbol", map[string]any{"query": query}, false)
	if err != nil || res == nil {
		return nil, err
	}
	var out []SymbolInfo
	json.Unmarshal(res, &out)
	return out, nil
}

// Definition returns the definition location(s) of the symbol at pos.
func (c *Client) Definition(file string, pos Position) ([]Location, error) {
	c.Open(file)
	res, err := c.send("textDocument/definition", posParams(file, pos), false)
	if err != nil || res == nil {
		return nil, err
	}
	return parseLocations(res), nil
}

// References returns all references to the symbol at pos.
func (c *Client) References(file string, pos Position, includeDecl bool) ([]Location, error) {
	c.Open(file)
	p := posParams(file, pos)
	p["context"] = map[string]any{"includeDeclaration": includeDecl}
	res, err := c.send("textDocument/references", p, false)
	if err != nil || res == nil {
		return nil, err
	}
	return parseLocations(res), nil
}

// PrepareCallHierarchy resolves the symbol at pos into a call-hierarchy item.
func (c *Client) PrepareCallHierarchy(file string, pos Position) ([]CallHierarchyItem, error) {
	c.Open(file)
	res, err := c.send("textDocument/prepareCallHierarchy", posParams(file, pos), false)
	if err != nil || res == nil {
		return nil, err
	}
	var out []CallHierarchyItem
	json.Unmarshal(res, &out)
	return out, nil
}

// IncomingCalls returns the callers of item (function-level).
func (c *Client) IncomingCalls(item CallHierarchyItem) ([]CallHierarchyItem, error) {
	res, err := c.send("callHierarchy/incomingCalls", map[string]any{"item": item}, false)
	if err != nil || res == nil {
		return nil, err
	}
	var raw []struct {
		From CallHierarchyItem `json:"from"`
	}
	json.Unmarshal(res, &raw)
	out := make([]CallHierarchyItem, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.From)
	}
	return out, nil
}

// OutgoingCalls returns the callees of item.
func (c *Client) OutgoingCalls(item CallHierarchyItem) ([]CallHierarchyItem, error) {
	res, err := c.send("callHierarchy/outgoingCalls", map[string]any{"item": item}, false)
	if err != nil || res == nil {
		return nil, err
	}
	var raw []struct {
		To CallHierarchyItem `json:"to"`
	}
	json.Unmarshal(res, &raw)
	out := make([]CallHierarchyItem, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.To)
	}
	return out, nil
}

// DocumentSymbol returns the outline of a file.
func (c *Client) DocumentSymbol(file string) (json.RawMessage, error) {
	c.Open(file)
	return c.send("textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)},
	}, false)
}

// Hover returns hover info (used for macro expansion / signatures).
func (c *Client) Hover(file string, pos Position) (json.RawMessage, error) {
	c.Open(file)
	return c.send("textDocument/hover", posParams(file, pos), false)
}

// Rename performs a workspace-wide rename of the symbol at pos.
func (c *Client) Rename(file string, pos Position, newName string) (json.RawMessage, error) {
	c.Open(file)
	p := posParams(file, pos)
	p["newName"] = newName
	return c.send("textDocument/rename", p, false)
}

// Close shuts down clangd.
func (c *Client) Close() {
	c.send("shutdown", nil, false)
	c.send("exit", nil, true)
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}

func posParams(file string, pos Position) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)},
		"position":     pos,
	}
}

func parseLocations(res json.RawMessage) []Location {
	var locs []Location
	if json.Unmarshal(res, &locs) == nil && len(locs) > 0 {
		return locs
	}
	// LocationLink[] fallback
	var links []struct {
		TargetURI   string `json:"targetUri"`
		TargetRange Range  `json:"targetRange"`
	}
	if json.Unmarshal(res, &links) == nil {
		for _, l := range links {
			locs = append(locs, Location{URI: l.TargetURI, Range: l.TargetRange})
		}
	}
	return locs
}

func pathToURI(p string) string {
	abs, _ := filepath.Abs(p)
	abs = filepath.ToSlash(abs)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs // windows drive
	}
	return "file://" + abs
}

// URIToPath converts a file:// uri back to a local path.
func URIToPath(uri string) string {
	p := strings.TrimPrefix(uri, "file://")
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:] // windows /C:/...
	}
	return filepath.FromSlash(p)
}
