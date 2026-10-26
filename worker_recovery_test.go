package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMCPWorkerCrashDoesNotKillReachableObjectProcesses(t *testing.T) {
	srv := startNATSTestServerOnPort(t, -1)
	t.Cleanup(srv.Shutdown)
	base := startMCPTestCoordinator(t, srv.ClientURL(), t.TempDir())
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("expected coordinator registry: %+v", objects)
	} else {
		// The worker joins the test coordinator's isolated NATS namespace.
	}
	cluster := strings.Split(entries[0].Subject, ".")[1]
	bin, _ := os.Executable()
	port := freePort(t)
	cmd := exec.Command(bin, "worker", "--coordinator", base, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--verbose=false")
	cmd.Env = append(os.Environ(), "SMT_EMBED_NATS=", natsURLVar+"="+srv.ClientURL(), natsClusterVar+"="+cluster)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	} else {
		defer cmd.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waitForPing(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/ping")
	waitForWorkerCount(t, base+"/metrics", 1)
	setup := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "c := Counter new. c add: 19. c value"})
	if setup.IsError || setup.Content[0].Text != "19" {
		t.Fatalf("remote setup failed: %+v", setup)
	} else {
		registerRemoteProcessCleanup(t, base)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	} else {
		<-done
	}
	result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "c value"})
	if result.IsError || result.Content[0].Text != "19" {
		t.Fatalf("worker crash lost reachable object state: %+v", result)
	} else {
		// Pulses come from the coordinator; worker loss alone does not kill children.
	}
}

func registerRemoteProcessCleanup(t *testing.T, base string) {
	t.Helper()
	objects := mcpTestCall(t, base, "objects", MCPToolArguments{})
	var entries []ObjectEntry
	if err := json.Unmarshal([]byte(objects.Content[0].Text), &entries); err != nil {
		t.Fatal(err)
	} else {
		for _, entry := range entries {
			if entry.ID == coordinatorID {
				continue
			} else {
				inspection := mcpTestCall(t, base, "inspect", MCPToolArguments{ObjectID: entry.ID})
				var object InspectResponse
				if err := json.Unmarshal([]byte(inspection.Content[0].Text), &object); err != nil || object.Runtime == nil || object.Runtime.WorkerID == "" {
					t.Fatalf("missing worker placement: %+v", inspection)
				} else {
					pid := object.Runtime.PID
					t.Cleanup(func() {
						if process, err := os.FindProcess(pid); err == nil {
							process.Kill()
						} else {
							// The runtime may already have collected this child.
						}
					})
				}
			}
		}
	}
}

func TestStaleProbeRequiresRepeatedFailureAndResetsOnRecovery(t *testing.T) {
	c := &Coordinator{}
	now := time.Now()
	if c.confirmStaleFailure("object", now) || c.confirmStaleFailure("object", now.Add(time.Second)) {
		t.Fatal("a brief interruption allowed object eviction")
	} else if !c.confirmStaleFailure("object", now.Add(staleReconnectGrace)) {
		t.Fatal("repeated failure did not eventually allow eviction")
	} else {
		c.clearStaleFailure("object")
	}
	if c.confirmStaleFailure("object", now.Add(time.Hour)) {
		t.Fatal("a recovered object retained stale suspicion")
	} else {
		// Later outages must establish a new suspicion window.
	}
}
