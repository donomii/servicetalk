package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPComputedPropertiesAndSetterErrors(t *testing.T) {
	dir := t.TempDir()
	if source, err := os.ReadFile("Thermometer.st"); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(filepath.Join(dir, "Thermometer.st"), source, 0600); err != nil {
		t.Fatal(err)
	} else {
		// Run the example through the real process-based object runtime.
	}
	broker := startNATSTestServerOnPort(t, -1)
	t.Cleanup(broker.Shutdown)
	base := startMCPTestCoordinator(t, broker.ClientURL(), dir)
	for _, test := range []struct{ code, want string; failure bool }{
		{"thermometer := Thermometer new. thermometer fahrenheit", "68", false},
		{"thermometer fahrenheit: 77. thermometer celsius", "25", false},
		{"thermometer celsius: -300", "absolute zero", true},
		{"thermometer celsius", "25", false},
		{"thermometer inspectorMetadata", "\"setter\":\"fahrenheit:\"", false},
	} {
		result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: test.code})
		if result.IsError != test.failure || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, test.want) {
			t.Fatalf("%s: expected %q failure=%v; got %+v", test.code, test.want, test.failure, result)
		} else {
			// Failed setters preserve the last accepted value.
		}
	}
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.ClassName == "Thermometer" {
				result := mcpTestCall(t, base, "inspect", MCPToolArguments{ObjectID: entry.ID})
				var inspection InspectResponse
				if err := json.Unmarshal([]byte(result.Content[0].Text), &inspection); err != nil {
					t.Fatal(err)
				} else if names := inspection.MethodSignatures["celsius:"]; len(names) != 1 || names[0] != "degrees" {
					t.Fatalf("expected degrees parameter, got %#v", names)
				} else {
					return
				}
			} else {
				// Other runtime objects do not define the example's properties.
			}
		}
	}
	t.Fatal("Thermometer missing from registry")
}
