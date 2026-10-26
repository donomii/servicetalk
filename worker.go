package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Worker is a cluster node that accepts spawn requests from the coordinator
// and runs object processes locally on its machine.
type Worker struct {
	id             string
	host           string
	port           int
	coordinatorURL string
	childrenMu     sync.Mutex
	children       map[string]*localChild
}

func runWorker(coordinatorURL, host string, port int) {
	if host == "" {
		host = localHostname()
	}

	w := &Worker{
		id:             newUUID(),
		host:           host,
		port:           port,
		coordinatorURL: coordinatorURL,
		children:       make(map[string]*localChild),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/spawn", w.handleSpawn)
	mux.HandleFunc("/shutdown", w.handleShutdown)
	mux.HandleFunc("/ping", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[worker %s] server error: %v\n", w.id, err)
			os.Exit(1)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	logInfo("[worker %s] listening on %s:%d, object transport %s\n", w.id, host, port, natsURL())

	w.sendHeartbeat() // immediate, so the coordinator can route to us without waiting a tick
	go w.heartbeatLoop()

	select {} // block forever
}

func (w *Worker) handleShutdown(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(rw, "worker: shutting down")
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		w.shutdownLocalChildren(5 * time.Second)
		time.Sleep(50 * time.Millisecond)
		os.Exit(0)
	}()
}

// sendHeartbeat posts a self-describing beat (id + host/port).  The
// coordinator upserts from it, so this is also registration: the first beat
// registers us, later beats keep us live, and any beat after a coordinator
// restart re-creates our entry.
func (w *Worker) sendHeartbeat() {
	req := WorkerHeartbeatRequest{WorkerID: w.id, Host: w.host, Port: w.port}
	body, _ := json.Marshal(req)
	resp, err := rpcClient.Post(w.coordinatorURL+"/worker-heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[worker %s] heartbeat failed: %v\n", w.id, err)
		return
	}
	resp.Body.Close()
}

func (w *Worker) heartbeatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		w.sendHeartbeat()
	}
}

// handleSpawn accepts a SpawnRequest from the coordinator, runs exec.Command
// locally, and waits for the object to register with the coordinator.
func (w *Worker) handleSpawn(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, internalHTTPMaxRequestBytes())
	var req SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(rw, "bad worker spawn request: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := w.spawnLocal(req)
	if err != nil {
		json.NewEncoder(rw).Encode(SpawnResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(rw).Encode(resp)
}

func (w *Worker) spawnLocal(req SpawnRequest) (SpawnResponse, error) {
	id := newUUID()

	bin, err := os.Executable()
	if err != nil {
		return SpawnResponse{}, fmt.Errorf("cannot locate binary: %w", err)
	}

	spawnClass := userToBuiltinClass(req.ClassName)

	pulseTimeoutSecs := req.PulseTimeoutSecs
	if pulseTimeoutSecs == 0 {
		pulseTimeoutSecs = 60
	}

	args := []string{
		"object",
		"--class", spawnClass,
		"--id", id,
		"--coordinator", w.coordinatorURL,
		"--pulse-timeout", strconv.Itoa(pulseTimeoutSecs),
	}
	if req.SourceBundle != nil {
		dir, err := stageSourceBundle(req.SourceBundle)
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("stage source bundle %s: %w", req.SourceBundle.Digest, err)
		}
		args = append(args, "--path", dir)
	}
	args = append(args, ClassPathArgs()...)
	if req.AutoInit {
		args = append(args, "--init")
	}

	// Write inline BlockDef to a local temp file for the child process
	if req.BlockDef != nil {
		data, err := json.Marshal(req.BlockDef)
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("marshal block def: %w", err)
		}
		tmp, err := os.CreateTemp("", "smt-block-"+id+"-*.json")
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("create block def: %w", err)
		}
		defFile := tmp.Name()
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return SpawnResponse{}, fmt.Errorf("write block def: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return SpawnResponse{}, fmt.Errorf("close block def: %w", err)
		}
		args = append(args, "--def", defFile)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "SMT_WORKER_ID="+w.id)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return SpawnResponse{}, fmt.Errorf("failed to start %s: %w", req.ClassName, err)
	}
	child := &localChild{id: id, done: make(chan error, 1)}
	w.childrenMu.Lock()
	w.children[id] = child
	w.childrenMu.Unlock()
	go func() {
		err := cmd.Wait()
		w.childrenMu.Lock()
		if w.children[id] == child {
			delete(w.children, id)
		}
		w.childrenMu.Unlock()
		child.done <- err
	}()

	// Wait for the object to appear in the coordinator registry
	if err := w.waitForRegistration(id, spawnTimeout()); err != nil {
		return SpawnResponse{}, err
	}

	return SpawnResponse{ObjectID: id, Subject: natsSubjectForObject(id)}, nil
}

func (w *Worker) shutdownLocalChildren(timeout time.Duration) {
	w.childrenMu.Lock()
	children := make([]*localChild, 0, len(w.children))
	for _, child := range w.children {
		children = append(children, child)
	}
	w.children = make(map[string]*localChild)
	w.childrenMu.Unlock()
	for _, child := range children {
		_, _ = sendNATSCall(natsSubjectForObject(child.id), "__shutdown", nil, 2*time.Second)
	}
	deadline := time.Now().Add(timeout)
	for _, child := range children {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case <-child.done:
		case <-time.After(remaining):
			return
		}
	}
}

// waitForRegistration polls the coordinator registry until the object appears.
func (w *Worker) waitForRegistration(objectID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := rpcClient.Get(w.coordinatorURL + "/registry")
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var hb HeartbeatResponse
		json.NewDecoder(resp.Body).Decode(&hb)
		resp.Body.Close()

		for _, entry := range hb.Registry {
			if entry.ID == objectID {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s to register with coordinator", objectID)
}
