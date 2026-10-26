package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReserveWorkerSpreadsSequentialSpawns(t *testing.T) {
	c := &Coordinator{workers: map[string]*WorkerEntry{
		"b": {ID: "b", Host: "127.0.0.1", Port: 9002},
		"a": {ID: "a", Host: "127.0.0.1", Port: 9001},
	}}

	first := c.reserveWorker()
	if first == nil || first.ID != "a" {
		t.Fatalf("first worker = %+v, want a", first)
	}
	c.releaseWorker(first.ID)

	second := c.reserveWorker()
	if second == nil || second.ID != "b" {
		t.Fatalf("second worker = %+v, want b", second)
	}
	c.releaseWorker(second.ID)
}

func TestReserveWorkerPrefersLowerInFlight(t *testing.T) {
	c := &Coordinator{workers: map[string]*WorkerEntry{
		"busy": {ID: "busy", Host: "127.0.0.1", Port: 9001, InFlightSpawns: 4},
		"idle": {ID: "idle", Host: "127.0.0.1", Port: 9002, InFlightSpawns: 1},
	}}

	worker := c.reserveWorker()
	if worker == nil || worker.ID != "idle" {
		t.Fatalf("worker = %+v, want idle", worker)
	}
	if c.workers["idle"].InFlightSpawns != 2 {
		t.Fatalf("idle in-flight = %d, want 2", c.workers["idle"].InFlightSpawns)
	}
	c.releaseWorker(worker.ID)
	if c.workers["idle"].InFlightSpawns != 1 {
		t.Fatalf("idle in-flight after release = %d, want 1", c.workers["idle"].InFlightSpawns)
	}
}

func TestHandleEvalRejectsOversizedBody(t *testing.T) {
	t.Setenv(internalHTTPMaxRequestBytesVar, "8")
	c := &Coordinator{}
	req := httptest.NewRequest(http.MethodPost, "/eval", strings.NewReader(`{"code":"123456789"}`))
	rec := httptest.NewRecorder()

	c.handleEval(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with EvalResponse error", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "http: request body too large") {
		t.Fatalf("body = %q, want body-too-large error", rec.Body.String())
	}
}

func TestWorkerSpawnRejectsOversizedBody(t *testing.T) {
	t.Setenv(internalHTTPMaxRequestBytesVar, "8")
	w := &Worker{}
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader(`{"className":"Counter"}`))
	rec := httptest.NewRecorder()

	w.handleSpawn(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "http: request body too large") {
		t.Fatalf("body = %q, want body-too-large error", rec.Body.String())
	}
}

func TestEvalTimeoutConfiguration(t *testing.T) {
	t.Setenv(evalTimeoutVar, "125000")
	if got := evalTimeout(); got != 125*time.Second {
		t.Fatalf("evalTimeout = %s, want 2m5s", got)
	}
}
