package mcp

import "testing"

// TestUnknownMethodError: an unknown method (with an id) gets a JSON-RPC
// "method not found" (-32601) error, not a crash or a dropped request.
func TestUnknownMethodError(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":11,"method":"no/such/method","params":{}}`)
	e, ok := r[11]["error"].(map[string]any)
	if !ok {
		t.Fatal("unknown method should produce a JSON-RPC error")
	}
	if e["code"].(float64) != -32601 {
		t.Errorf("unknown method code = %v, want -32601", e["code"])
	}
}

// TestToolsCallMissingParams: tools/call with no params at all is an invalid
// request (-32602), reported as a compliant error.
func TestToolsCallMissingParams(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call"}`)
	e, ok := r[12]["error"].(map[string]any)
	if !ok {
		t.Fatal("tools/call with no params should produce a JSON-RPC error")
	}
	if e["code"].(float64) != -32602 {
		t.Errorf("missing params code = %v, want -32602", e["code"])
	}
}

// TestToolsCallInvalidParams: params present but the wrong shape (a JSON string
// instead of an object) is rejected with -32602.
func TestToolsCallInvalidParams(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":13,"method":"tools/call","params":"oops"}`)
	e, ok := r[13]["error"].(map[string]any)
	if !ok {
		t.Fatal("tools/call with non-object params should produce a JSON-RPC error")
	}
	if e["code"].(float64) != -32602 {
		t.Errorf("invalid params code = %v, want -32602", e["code"])
	}
}

// TestMalformedLineIgnoredNoCrash: a garbage (non-JSON) line must not crash the
// server or stall the loop — a following valid request is still answered, and the
// garbage line itself produces no response.
func TestMalformedLineIgnoredNoCrash(t *testing.T) {
	r := drive(t, okRunner,
		`this is not json {{{`,
		`{"jsonrpc":"2.0","id":14,"method":"ping"}`)
	if _, ok := r[14]; !ok {
		t.Error("server should keep serving after a malformed line")
	}
	if len(r) != 1 {
		t.Errorf("the malformed line should produce no response; got %v", r)
	}
}

// TestEmptyToolNameIsError: tools/call with an empty/absent tool name is an
// unknown tool, surfaced as a JSON-RPC error.
func TestEmptyToolNameIsError(t *testing.T) {
	r := drive(t, okRunner,
		`{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"arguments":{"symbol":"x"}}}`)
	if _, ok := r[15]["error"]; !ok {
		t.Error("tools/call with no tool name should produce a JSON-RPC error")
	}
}
