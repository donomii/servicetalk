package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMCPProtocol(t *testing.T) {
	c := &Coordinator{registry: map[string]*ObjectEntry{}}
	for _, tc := range []struct{ body, contains string; status int }{
		{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, `"protocolVersion":"2025-06-18"`, 200},
		{`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, `-32602`, 200},
		{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, `-32600`, 200},
		{`{"jsonrpc":"2.0","id":{},"method":"ping"}`, `-32600`, 200},
		{`{"jsonrpc":"2.0","id":true,"method":"ping"}`, `-32600`, 200},
		{`{"jsonrpc":"2.0","id":1.5,"method":"ping"}`, `-32600`, 200},
		{`{"jsonrpc":"2.0","id":1,"method":"ping","params":null}`, `-32600`, 200},
		{`{"jsonrpc":"2.0","id":"list","method":"tools/list"}`, `"name":"inspect"`, 200},
		{`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "", 202},
		{`{"jsonrpc":"2.0","id":2,"method":"absent"}`, `-32601`, 200},
		{`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"objects","arguments":{}}}`, `"isError":false`, 200},
		{`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"call","arguments":{"objectId":"absent","method":"class"}}}`, `"isError":true`, 200},
		{`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"objects","arguments":{"typo":1}}}`, `-32602`, 200},
		{`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"eval","arguments":{}}}`, `-32602`, 200},
		{`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"inspect","arguments":{"objectId":"coordinator"}}}`, `"isError":false`, 200},
		{`[`, `-32700`, 200},
		{`[]`, `-32600`, 200},
	} {
		w := httptest.NewRecorder()
		c.handleMCP(w, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(tc.body)))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatalf("request %s: got %d %s, expected %d containing %s", tc.body, w.Code, w.Body.String(), tc.status, tc.contains)
		} else {
			// Each response is independently usable by a stateless client.
		}
	}
}

func TestMCPHTTPNegotiation(t *testing.T) {
	c := &Coordinator{}
	for _, tc := range []struct{ method, origin, version string; status int }{
		{http.MethodGet, "", "", 405},
		{http.MethodPost, "https://elsewhere.invalid", "", 403},
		{http.MethodPost, "", "unsupported", 400},
	} {
		r := httptest.NewRequest(tc.method, "http://localhost/mcp", strings.NewReader(`{}`))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("MCP-Protocol-Version", tc.version)
		w := httptest.NewRecorder()
		c.handleMCP(w, r)
		if w.Code != tc.status {
			t.Fatalf("got status %d, expected %d", w.Code, tc.status)
		} else {
			// Unsupported transports and versions do not execute tools.
		}
	}
}

func mcpTestCall(t *testing.T, base, name string, args MCPToolArguments) MCPToolResult {
	t.Helper()
	arguments, _ := json.Marshal(args)
	params, _ := json.Marshal(MCPToolCall{Name: name, Arguments: arguments})
	body, _ := json.Marshal(MCPRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params})
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(base+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	} else {
		defer resp.Body.Close()
	}
	var envelope MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	} else if envelope.Error != nil {
		t.Fatalf("MCP error: %+v", envelope.Error)
	} else {
		var result MCPToolResult
		if err := json.Unmarshal(envelope.Result, &result); err != nil {
			t.Fatal(err)
		} else {
			return result
		}
	}
	return MCPToolResult{}
}

func startMCPTestCoordinator(t *testing.T, broker, directory string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	} else {
		// TestMain dispatches the child as a normal runtime.
	}
	port := freePort(t)
	cmd := exec.Command(bin, "coordinator", "--port", strconv.Itoa(port), "--main", "", "--path", directory, "--verbose=false")
	cmd.Env = append(os.Environ(), "SMT_EMBED_NATS=", natsURLVar+"="+broker, "SMT_HOST=127.0.0.1", "SMT_REGISTRY_FILE=", natsClusterVar+"="+newUUID())
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	} else {
		base := "http://127.0.0.1:" + strconv.Itoa(port)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		t.Cleanup(func() { postShutdownAndWait(t, "MCP coordinator", base, done) })
		waitForPing(t, base+"/ping")
		waitForMCPRegistry(t, base)
		return base
	}
	return ""
}

func waitForMCPRegistry(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(5*time.Second)
	for time.Now().Before(deadline) {
		result := mcpTestCall(t, base, "objects", MCPToolArguments{})
		if len(result.Content) == 1 && strings.Contains(result.Content[0].Text, `"id":"coordinator"`) {
			return
		} else {
			time.Sleep(10*time.Millisecond)
		}
	}
	t.Fatal("coordinator did not register its MCP runtime")
}

func TestMCPRealObjectsAndSignals(t *testing.T) {
	srv := startNATSTestServerOnPort(t, -1)
	t.Cleanup(srv.Shutdown)
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/Failure.st", []byte("class Failure [ initialize [ nil ] fail [ 'nested method failure' signal ] ]"), 0600); err != nil {
		t.Fatal(err)
	} else {
		// The class is loaded by an actual object subprocess.
	}
	base := startMCPTestCoordinator(t, srv.ClientURL(), dir)
	for _, tc := range []struct{ code, want string; fails bool }{
		{"answer := 40. answer + 2", "42", false},
		{"answer + 3", "43", false},
		{"Failure new fail", "nested method failure", true},
		{"({Failure new. Failure new} parallelSend: #fail withEach: {nil. nil})", "nested method failure", true},
		{"[ 'nested block failure' signal ] value", "nested block failure", true},
		{"[ Failure new fail ] onError: [e | 'handled: ' , e]", "handled: nested method failure", false},
		{"6 * 7", "42", false},
	} {
		result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: tc.code})
		if result.IsError != tc.fails || len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, tc.want) {
			t.Fatalf("eval %s: %+v; want failure=%v, %s", tc.code, result, tc.fails, tc.want)
		} else {
			// Errors leave the workspace usable for the following call.
		}
	}
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.ClassName == BuiltinClassWorkspace {
				checkMCPInspection(t, base, entry.ID)
			} else {
				// Inspect the root workspace; other objects may be temporary.
			}
		}
	}
}

func checkMCPInspection(t *testing.T, base, id string) {
	t.Helper()
	result := mcpTestCall(t, base, "inspect", MCPToolArguments{ObjectID: id})
	var inspection InspectResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &inspection); err != nil {
		t.Fatal(err)
	} else if result.IsError || inspection.Runtime.PID == os.Getpid() || inspection.Runtime.PID == 0 || !inspection.Retention.Root {
		t.Fatalf("expected a separate workspace process and a GC root: %+v", inspection)
	} else {
		// This is the real process-based runtime, reached entirely through MCP.
	}
}
