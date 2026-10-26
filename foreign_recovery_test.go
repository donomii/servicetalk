package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMCPForeignSignalsAndHeldReferences(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("foreign integration requires a C compiler")
	} else {
		// Build and run the same foreign protocol used by ordinary C classes.
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Keeper"), 0700); err != nil {
		t.Fatal(err)
	} else {
		writeForeignRecoveryFixture(t, dir)
	}
	srv := startNATSTestServerOnPort(t, -1)
	t.Cleanup(srv.Shutdown)
	base := startMCPTestCoordinator(t, srv.ClientURL(), dir)
	result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "keeper := Keeper new. target := Counter new. keeper hold: target. target := nil"})
	if result.IsError {
		t.Fatalf("foreign setup: %+v", result)
	} else {
		// The target is now held exclusively in C state.
	}
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	} else {
		var keeperID, targetID string
		for _, entry := range entries {
			switch entry.ClassName {
			case "Keeper": keeperID = entry.ID
			case "Counter": targetID = entry.ID
			}
		}
		checkForeignPulseAndFailure(t, base, keeperID, targetID)
	}
}

func writeForeignRecoveryFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"Keeper/Keeper.smt": `(foreign-class Keeper (cmethod initialize init_keeper) (cmethod hold: hold) (cmethod pulse: pulse) (cmethod fail fail))`,
		"Keeper/keeper.c": `#include "_st.h"
static Value held;
Value init_keeper(int n, Value* a) { held = st_nil(); return held; }
Value hold(int n, Value* a) { held = a[0]; return st_nil(); }
Value pulse(int n, Value* a) { if (st_isObject(held)) { return st_send(held, "pulse:", n, a); } else { return st_nil(); } }
Value fail(int n, Value* a) { st_signal("foreign failure"); return st_nil(); }
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		} else {
			// Generated glue and binaries remain inside the test directory.
		}
	}
}

func checkForeignPulseAndFailure(t *testing.T, base, keeperID, targetID string) {
	t.Helper()
	sweep := time.Now().Add(time.Hour).UnixMilli()
	result := mcpTestCall(t, base, "call", MCPToolArguments{ObjectID: keeperID, Method: "pulse:", Args: []Value{NumVal(float64(sweep))}})
	inspection := mcpTestCall(t, base, "inspect", MCPToolArguments{ObjectID: targetID})
	var target InspectResponse
	if err := json.Unmarshal([]byte(inspection.Content[0].Text), &target); err != nil {
		t.Fatal(err)
	} else if result.IsError || target.Runtime.LastSweepID != sweep || len(target.Retention.Path) != 0 {
		t.Fatalf("foreign pulse did not retain the hidden reference correctly: %+v, %+v", result, target)
	} else {
		// Reachability is real even when the generic inspector cannot see C state.
	}
	result = mcpTestCall(t, base, "eval", MCPToolArguments{Code: "keeper fail"})
	if !result.IsError || !strings.Contains(result.Content[0].Text, "foreign failure") {
		t.Fatalf("foreign signal was swallowed: %+v", result)
	} else {
		result = mcpTestCall(t, base, "eval", MCPToolArguments{Code: "[ keeper fail ] onError: [e | 'handled: ' , e]"})
	}
	if result.IsError || result.Content[0].Text != "handled: foreign failure" {
		t.Fatalf("foreign signal could not be handled: %+v", result)
	} else {
		// The bridge remains usable after a signal.
	}
}
