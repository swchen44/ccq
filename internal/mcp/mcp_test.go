package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// drive feeds newline-delimited requests through Serve and returns the parsed
// JSON-RPC responses, keyed by id.
func drive(t *testing.T, run Runner, requests ...string) map[float64]map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out strings.Builder
	if err := Serve(in, &out, run, "/tmp/proj"); err != nil {
		t.Fatal(err)
	}
	res := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON response: %q", line)
		}
		if id, ok := m["id"].(float64); ok {
			res[id] = m
		}
	}
	return res
}

func okRunner(cmd, arg, root string) (string, error) {
	return cmd + ":" + arg + "@" + root, nil
}

func TestInitializeAndToolsList(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if r[1]["result"].(map[string]any)["protocolVersion"] != protocolVersion {
		t.Error("initialize missing protocolVersion")
	}
	tools := r[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(Tools) {
		t.Errorf("tools/list returned %d tools, want %d", len(tools), len(Tools))
	}
	// explore must be present (CodeGraph-compatible headline)
	found := false
	for _, tl := range tools {
		if tl.(map[string]any)["name"] == "explore" {
			found = true
		}
	}
	if !found {
		t.Error("tools/list missing the explore tool")
	}
}

func TestToolsCallRoutesToRunner(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"callers","arguments":{"symbol":"foo","path":"/r"}}}`)
	result := r[5]["result"].(map[string]any)
	if result["isError"] != false {
		t.Error("expected isError=false")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"]
	if text != "callers:foo@/r" {
		t.Errorf("runner routing wrong: %v", text)
	}
}

func TestToolsCallDefaultRoot(t *testing.T) {
	// omitting path falls back to the server default root
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"def","arguments":{"symbol":"bar"}}}`)
	text := r[6]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if text != "def:bar@/tmp/proj" {
		t.Errorf("default root not used: %v", text)
	}
}

func TestUnknownToolIsError(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nope","arguments":{"symbol":"x"}}}`)
	if _, ok := r[7]["error"]; !ok {
		t.Error("unknown tool should produce a JSON-RPC error")
	}
}

func TestNotificationNoResponse(t *testing.T) {
	// notifications have no id and must not produce a response
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if _, ok := r[9]; !ok {
		t.Error("ping should be answered")
	}
	if len(r) != 1 {
		t.Errorf("notification produced an unexpected response: %v", r)
	}
}
