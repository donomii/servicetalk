package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPInspectionShowsLiveCallsAndWaits(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, "response")
	}))
	t.Cleanup(httpServer.Close)
	srv := startNATSTestServerOnPort(t, -1)
	t.Cleanup(srv.Shutdown)
	base := startMCPTestCoordinator(t, srv.ClientURL(), t.TempDir())
	setup := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "client := HttpClient new"})
	if setup.IsError {
		t.Fatalf("HTTP client setup: %+v", setup)
	} else {
		// Start a call whose remote HTTP response remains under test control.
	}
	done := make(chan MCPToolResult, 1)
	t.Cleanup(func() {
		close(release)
		select {
		case <-done:
		case <-time.After(5*time.Second): t.Error("MCP call did not finish after HTTP response release")
		}
	})
	go func() { done <- mcpTestCall(t, base, "eval", MCPToolArguments{Code: "client get: '"+httpServer.URL+"'"}) }()
	select {
	case <-entered:
		checkWaitingWorkspace(t, base)
	case <-time.After(5*time.Second):
		t.Fatal("HTTP request did not start")
	}
	// Cleanup releases the request before shutting down the runtime.
}

func checkWaitingWorkspace(t *testing.T, base string) {
	t.Helper()
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.ClassName == BuiltinClassWorkspace {
				inspection := mcpTestCall(t, base, "inspect", MCPToolArguments{ObjectID: entry.ID})
				if inspection.IsError || !strings.Contains(inspection.Content[0].Text, `"state":"waiting"`) || !strings.Contains(inspection.Content[0].Text, `"state":"executing"`) {
					t.Fatalf("inspection lost live call/wait information: %+v", inspection)
				} else {
					return
				}
			} else {
				// Locate the workspace without relying on registry ordering.
			}
		}
	}
	t.Fatal("workspace not found")
}
