// Package mcp implements a minimal Model Context Protocol server (JSON-RPC 2.0
// over newline-delimited stdio) that exposes ccq's navigation as MCP tools, so
// agents/IDEs that speak MCP — and users familiar with CodeGraph — can drive ccq
// with zero new dependencies. Headline tool: `explore` (CodeGraph-compatible).
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

const protocolVersion = "2024-11-05"

// Runner executes a ccq command (cmd) with a single argument (arg) against the
// project root, returning the text output. Provided by the caller so this package
// stays free of clangd/daemon wiring.
type Runner func(cmd, arg, root string) (string, error)

// Tool is a ccq command exposed over MCP.
type Tool struct {
	Name string
	Desc string
	Cmd  string // ccq subcommand
	Arg  string // human description of the single argument
}

// Tools mirrors ccq's read-only navigation; `explore` is the CodeGraph-compatible
// one-shot. Names match ccq subcommands so the mapping is obvious.
var Tools = []Tool{
	{"explore", "One-shot context for a C/C++ symbol: source + callers + callees + blast radius (CodeGraph-compatible).", "explore", "symbol name"},
	{"callers", "Who calls this function — clangd call hierarchy + fn-pointer dispatch.", "callers", "function name"},
	{"callees", "What this function calls.", "callees", "function name"},
	{"def", "Show a symbol's definition source.", "def", "symbol name"},
	{"refs", "All references to a symbol.", "refs", "symbol name"},
	{"search", "Find symbols by name.", "search", "query"},
	{"impact", "Transitive callers of a symbol (blast radius).", "impact", "symbol name"},
	{"symbols", "Outline the symbols in a file.", "symbols", "file path"},
	{"macro", "Macro expansion / signature.", "macro", "macro or symbol name"},
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Serve runs the MCP loop until in hits EOF. defaultRoot is used when a tool call
// omits "path".
func Serve(in io.Reader, out io.Writer, run Runner, defaultRoot string) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()
	send := func(id json.RawMessage, result any, errObj *rpcError) {
		msg := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
		if errObj != nil {
			msg["error"] = errObj
		} else {
			msg["result"] = result
		}
		b, _ := json.Marshal(msg)
		w.Write(b)
		w.WriteByte('\n')
		w.Flush()
	}
	for {
		line, err := r.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var m rpcMsg
			if json.Unmarshal(line, &m) == nil {
				handle(m, send, run, defaultRoot)
			}
		}
		if err != nil {
			return nil // EOF or read error → done
		}
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handle(m rpcMsg, send func(json.RawMessage, any, *rpcError), run Runner, defaultRoot string) {
	switch m.Method {
	case "initialize":
		send(m.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ccq", "version": "0.5.0"},
		}, nil)
	case "tools/list":
		send(m.ID, map[string]any{"tools": toolSchemas()}, nil)
	case "tools/call":
		id, result, errObj := callTool(m.Params, run, defaultRoot)
		send(id(m.ID), result, errObj)
	case "ping":
		send(m.ID, map[string]any{}, nil)
	case "notifications/initialized", "notifications/cancelled":
		// notifications: no response
	default:
		if len(m.ID) > 0 {
			send(m.ID, nil, &rpcError{Code: -32601, Message: "method not found: " + m.Method})
		}
	}
}

func toolSchemas() []map[string]any {
	var out []map[string]any
	for _, t := range Tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Desc,
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string", "description": t.Arg},
					"path":   map[string]any{"type": "string", "description": "project directory (default: server working dir)"},
				},
				"required": []string{"symbol"},
			},
		})
	}
	return out
}

// callTool returns an id-selector (so the response keeps the request id), the
// result, and any error.
func callTool(params json.RawMessage, run Runner, defaultRoot string) (func(json.RawMessage) json.RawMessage, any, *rpcError) {
	keep := func(id json.RawMessage) json.RawMessage { return id }
	var p struct {
		Name      string `json:"name"`
		Arguments struct {
			Symbol string `json:"symbol"`
			Path   string `json:"path"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return keep, nil, &rpcError{Code: -32602, Message: "invalid params"}
	}
	var tool *Tool
	for i := range Tools {
		if Tools[i].Name == p.Name {
			tool = &Tools[i]
			break
		}
	}
	if tool == nil {
		return keep, nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
	root := p.Arguments.Path
	if root == "" {
		root = defaultRoot
	}
	text, err := run(tool.Cmd, p.Arguments.Symbol, root)
	isErr := false
	if err != nil {
		text, isErr = "error: "+err.Error(), true
	}
	return keep, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}, nil
}
