package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPRejectsInvalidValuesBeforeDispatch(t *testing.T) {
	for _, arguments := range []string{
		`{"args":[{}]}`,
		`{"args":[null]}`,
		`{"args":[{"kind":"unknown"}]}`,
		`{"args":[{"kind":"character","char":55296}]}`,
		`{"args":[{"kind":"number","num":"invalid"}]}`,
	} {
		var args MCPToolArguments
		if err := decodeMCPArguments(json.RawMessage(arguments), &args); err == nil {
			t.Fatalf("accepted invalid Value: %s", arguments)
		} else {
			// Invalid calls never reach an object's method dispatcher.
		}
	}
}

func TestMCPRequestSizeLimit(t *testing.T) {
	t.Setenv(natsMaxCallBytesVar, "64")
	c := &Coordinator{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", 65)))
	c.handleMCP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request: got status %d, expected 413", w.Code)
	} else {
		// Report the size limit rather than a misleading JSON parse error.
	}
}

func TestMCPVersionNegotiation(t *testing.T) {
	c := &Coordinator{}
	response := c.dispatchMCP(MCPRequest{JSONRPC: "2.0", ID: json.RawMessage(`"init"`), Method: "initialize", Params: json.RawMessage(`{"protocolVersion":"future-version","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`)})
	if response.Error != nil || !strings.Contains(string(response.Result), mcpVersion) {
		t.Fatalf("server failed to offer its supported version: %+v", response)
	} else {
		// A newer client can negotiate the server's supported version.
	}
}
