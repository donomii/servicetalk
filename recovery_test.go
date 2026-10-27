package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPulseLeaseUsesLocalArrivalDespiteRemoteClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{-24*time.Hour, 24*time.Hour} {
		obj := newTestObject("Clock")
		obj.pulseTimeout = time.Minute
		obj.recordPulseArrival(time.Now().Add(-time.Hour))
		sweep := time.Now().Add(skew).UnixMilli()
		if !obj.observePulse(sweep) || obj.pulseAge(time.Now()) >= time.Second {
			t.Fatalf("fresh pulse with clock offset %s did not renew local lease", skew)
		} else if obj.pulseAge(time.Now().Add(2*time.Minute)) < obj.pulseTimeout {
			t.Fatal("a future coordinator timestamp prevented timeout after a partition")
		} else {
			// Remote time neither expires fresh objects nor grants extra lifetime.
		}
	}
}

func TestRecoveredWorkspaceRetainsMainIdentity(t *testing.T) {
	c := &Coordinator{registry: map[string]*ObjectEntry{"main": {ID:"main",ClassName:BuiltinClassWorkspace},"example": {ID:"example",ClassName:BuiltinClassWorkspace},"counter": {ID:"counter",ClassName:"Counter"}}, roots: map[string]struct{}{"main":{},"counter":{}}}
	if id,err := c.ensureWorkspace(); err != nil || id != "main" {
		t.Fatalf("expected surviving main workspace without spawning a replacement, got %q: %v",id,err)
	} else {}
	c.workspaceID = ""
	c.roots["example"] = struct{}{}
	if _,err := c.ensureWorkspace(); err == nil || !strings.Contains(err.Error(),"pinned workspace roots") {
		t.Fatalf("ambiguous restored workspaces should report their roots: %v",err)
	} else {}
	delete(c.registry,"main")
	delete(c.registry,"example")
	if id,err := c.recoveredWorkspace(); err != nil || id != "" {
		t.Fatalf("dead registry roots should not be reused: %q %v",id,err)
	} else {}
}

func TestDuplicatePulseCannotKeepPartitionedObjectAlive(t *testing.T) {
	obj := newTestObject("Clock")
	obj.observePulse(123)
	before := obj.pulseAge(time.Now())
	time.Sleep(5*time.Millisecond)
	if obj.observePulse(123) || obj.observePulse(122) || obj.pulseAge(time.Now()) <= before {
		t.Fatal("stale/replayed pulses renewed the lease or propagated")
	} else {
		// A missed stream of new sweeps eventually expires the lease.
	}
}

func TestWorkerHeartbeatRepopulatesRestartedCoordinator(t *testing.T) {
	first := &Coordinator{workers: map[string]*WorkerEntry{}}
	second := &Coordinator{workers: map[string]*WorkerEntry{}}
	body, _ := json.Marshal(WorkerHeartbeatRequest{WorkerID: "worker", Host: "127.0.0.1", Port: 8001})
	for _, c := range []*Coordinator{first, second} {
		w := httptest.NewRecorder()
		c.handleWorkerHeartbeat(w, httptest.NewRequest(http.MethodPost, "/worker-heartbeat", bytes.NewReader(body)))
		worker := c.reserveWorker()
		if w.Code != 200 || worker == nil || worker.ID != "worker" {
			t.Fatalf("heartbeat failed to restore worker routing: status %d, worker %+v", w.Code, worker)
		} else {
			c.releaseWorker(worker.ID)
		}
	}
}

func TestUnavailableWorkerFailsSpawnAndReleasesReservation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "worker offline", 503) }))
	defer srv.Close()
	var port int
	if _, err := fmt.Sscanf(strings.TrimPrefix(srv.URL, "http://127.0.0.1:"), "%d", &port); err != nil {
		t.Fatal(err)
	} else {
		c := &Coordinator{workers: map[string]*WorkerEntry{"lost": {ID: "lost", Host: "127.0.0.1", Port: port}}}
		if _, err := c.spawnObject("Counter", nil, false, 60); err == nil {
			t.Fatal("worker loss was reported as successful spawn")
		} else if c.workers["lost"].InFlightSpawns != 0 {
			t.Fatal("failed worker retained its spawn reservation")
		} else {
			// Subsequent scheduling does not accumulate phantom in-flight work.
		}
	}
}

func TestMCPBrokerOutageAndReconnectPreservesObject(t *testing.T) {
	t.Setenv(evalTimeoutVar, "500")
	port := freePort(t)
	srv := startNATSTestServerOnPort(t, port)
	t.Cleanup(func() { srv.Shutdown() })
	base := startMCPTestCoordinator(t, srv.ClientURL(), t.TempDir())
	result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "c := Counter new. c add: 17. c value"})
	if result.IsError || result.Content[0].Text != "17" {
		t.Fatalf("setup failed: %+v", result)
	} else {
		srv.Shutdown()
	}
	result = mcpTestCall(t, base, "eval", MCPToolArguments{Code: "c value"})
	if !result.IsError {
		t.Fatalf("broker outage was not reported: %+v", result)
	} else {
		srv = startNATSTestServerOnPort(t, port)
	}
	deadline := time.Now().Add(10*time.Second)
	for time.Now().Before(deadline) {
		result = mcpTestCall(t, base, "eval", MCPToolArguments{Code: "c value"})
		if !result.IsError && result.Content[0].Text == "17" {
			return
		} else {
			time.Sleep(50*time.Millisecond)
		}
	}
	t.Fatalf("object did not reconnect with its existing state: %+v", result)
}
