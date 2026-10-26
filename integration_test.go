package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

// TestIntegrationEndToEnd boots a real coordinator (the test binary re-exec'd
// via the TestMain hook), then drives end-to-end Smalltalk through the /eval
// endpoint.  This is the only test that exercises the full path — spawning,
// RPC, block evaluation, the collection methods that build result collections,
// cascades, perform:, JSON, and error handling.  Skipped under -short.
func TestIntegrationEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	port := freePort(t)
	natsSrv, err := natsserver.NewServer(&natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("start NATS: %v", err)
	}
	go natsSrv.Start()
	if !natsSrv.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	defer natsSrv.Shutdown()

	cmd := exec.Command(bin,
		"coordinator",
		"--port", strconv.Itoa(port),
		"--main", "",
		"--verbose=false",
	)
	cmd.Env = append(os.Environ(), natsURLVar+"="+natsSrv.ClientURL())
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start coordinator: %v", err)
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	coordinatorDone := make(chan error, 1)
	go func() {
		coordinatorDone <- cmd.Wait()
	}()
	defer func() {
		postShutdownAndWait(t, "coordinator", base, coordinatorDone)
	}()

	waitForPing(t, base+"/ping")

	eval := func(code string) string {
		t.Helper()
		body, _ := json.Marshal(EvalRequest{Code: code})
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Post(base+"/eval", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("eval %q: POST failed: %v", code, err)
		}
		defer resp.Body.Close()
		var er EvalResponse
		if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
			t.Fatalf("eval %q: decode failed: %v", code, err)
		}
		if er.Error != "" {
			t.Fatalf("eval %q: error: %s", code, er.Error)
		}
		return strings.TrimSpace(er.Output)
	}

	cases := []struct {
		name string
		code string
		want string
	}{
		{"select+size", "({10. 20. 30. 40} select: [x | x > 20]) size", "2"},
		{"reject", "({1. 2. 3. 4} reject: [x | x > 2]) size", "2"},
		{"inject:into:", "{1. 2. 3. 4} inject: 0 into: [a b | a + b]", "10"},
		{"detect:ifNone:", "{1. 2. 3} detect: [x | x > 10] ifNone: [-1]", "-1"},
		{"sort", "a := {3. 1. 2}. a sort. a first", "1"},
		{"reversed", "({1. 2. 3} reversed) first", "3"},
		{"copy independent", "a := {1. 2}. b := a copy. b add: 3. a size", "2"},
		{"with:do:", "sum := 0. {1. 2. 3} with: {10. 20. 30} do: [a b | sum := sum + (a * b)]. sum", "140"},
		{"parallelSend:withEach: preserves order", "workers := {Counter new. Counter new. Counter new}. results := workers parallelSend: #add: withEach: {5. 7. 9}. results join: ','", "5,7,9"},
		{"parallelSend:withEach:timeoutMs: preserves order", "workers := {Counter new. Counter new}. results := workers parallelSend: #add: withEach: {11. 13} timeoutMs: 1000. results join: ','", "11,13"},
		{"cascade on new", "c := Counter new; increment; increment; yourself. c value", "2"},
		{"perform:with:", "c := Counter new. c perform: #add: with: 5. c value", "5"},
		{"dict collect:", "d := Dictionary new. d at: 'a' put: 2. e := d collect: [v | v * 10]. e at: 'a'", "20"},
		{"dict at:ifAbsentPut:", "d := Dictionary new. d at: 'k' ifAbsentPut: [42]. d at: 'k'", "42"},
		{"json round-trip", "a := JSON decode: '[10,20,30]'. a at: 1", "20"},
		{"onError:", "[ 'boom' signal ] onError: [e | 'caught: ' , e]", "caught: boom"},
		{"any: true", "{1. 2. 3} any: [x | x > 2]", "true"},
		{"any: false", "{1. 2. 3} any: [x | x > 10]", "false"},
		{"all: true", "{1. 2. 3} all: [x | x > 0]", "true"},
		{"all: false", "{1. 2. 3} all: [x | x > 1]", "false"},
		{"dict at:ifPresent:", "d := Dictionary new. d at: 'k' put: 99. d at: 'k' ifPresent: [v | v * 2]", "198"},
		{"inherit method", "c := Cat new. c speak", "cat says meow"},
		{"super initialize", "c := Cat new. c name", "cat"},
		{"super in override", "c := Cat new. c learnTrick. c learnTrick. c describe", "I am a cat that knows 2 tricks"},
		{"child-only method", "c := Cat new. c learnTrick. c trickCount", "1"},
		// Core evaluator paths: lazy control flow, blocks, loops, ensure:.
		{"ifTrue:ifFalse: true branch", "(3 > 2) ifTrue: ['yes'] ifFalse: ['no']", "yes"},
		{"ifTrue:ifFalse: false branch", "(1 > 2) ifTrue: ['yes'] ifFalse: ['no']", "no"},
		{"and: evaluates block", "true and: [false]", "false"},
		{"or: evaluates block", "false or: [false]", "false"},
		{"and: short circuits", "false and: ['unreached' signal]", "false"},
		{"or: short circuits", "true or: ['unreached' signal]", "true"},
		{"ifTrue: only-branch nil", "(1 > 2) ifTrue: ['yes']", "nil"},
		{"block value:value:", "blk := [a b | a + b]. blk value: 4 value: 5", "9"},
		{"whileTrue: accumulates", "i := 0. n := 0. [ i < 5 ] whileTrue: [ i := i + 1. n := n + i ]. n", "15"},
		{"timesRepeat:", "n := 0. 4 timesRepeat: [ n := n + 1 ]. n", "4"},
		{"ensure: runs cleanup", "log := Array new. [ log add: 'body' ] ensure: [ log add: 'cleanup' ]. log join: ','", "body,cleanup"},
		// ensure: cleanup runs on signal, then the signal propagates across the
		// block-to-block RPC boundary to the enclosing onError:. Regression test
		// for the latent hang where ensure:'s re-raise crashed the receiver's
		// handler goroutine and the caller blocked until the send timed out.
		{"ensure: cleanup runs on signal", "log := Array new. [ [ 'x' signal ] ensure: [ log add: 'cleanup' ] ] onError: [e | log add: 'caught' ]. log join: ','", "cleanup,caught"},
		{"to:do: sum", "s := 0. 1 to: 5 do: [i | s := s + i]. s", "15"},
		// perform: across receiver kinds + reflective dispatch.
		{"perform: on number", "5 perform: #factorial", "120"},
		{"perform:with: on number", "3 perform: #+ with: 4", "7"},
		{"perform: on string", "'hi' perform: #asUppercase", "HI"},
		{"perform:withArguments:", "d := Dictionary new. d perform: #at:put: withArguments: {'k'. 9}. d at: 'k'", "9"},
		// cascade returns the last message's value, not the receiver.
		{"cascade returns last", "{1. 2. 3} add: 4; add: 5; size", "5"},
		// doesNotUnderstand: forwarding — Proxy relays unknown sends via perform:.
		{"dNU forwarding", "p := Proxy new. p target: (Counter new). p increment. p increment. p value", "2"},
		// Character literals survive the JSON/RPC round-trip through a spawned Array.
		{"character in array", "a := {$x. $y. $z}. (a at: 1) asInteger", "121"},
		{"character method via array", "a := {$a. $b}. (a at: 0) asUppercase printString", "$A"},
		{"number to character", "(104 asCharacter) asString", "h"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(tc.code); got != tc.want {
				t.Errorf("eval %q = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}

func TestIntegrationCoordinatorWorkerSpawn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	coordPort := freePort(t)
	workerPort := freePort(t)
	natsSrv, err := natsserver.NewServer(&natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("start NATS: %v", err)
	}
	go natsSrv.Start()
	if !natsSrv.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	defer natsSrv.Shutdown()

	coordCmd := exec.Command(bin,
		"coordinator",
		"--port", strconv.Itoa(coordPort),
		"--main", "",
		"--verbose=false",
	)
	coordCmd.Env = append(os.Environ(), natsURLVar+"="+natsSrv.ClientURL())
	coordCmd.Stdout = os.Stderr
	coordCmd.Stderr = os.Stderr
	if err := coordCmd.Start(); err != nil {
		t.Fatalf("start coordinator: %v", err)
	}
	coordBase := fmt.Sprintf("http://localhost:%d", coordPort)
	coordDone := make(chan error, 1)
	go func() {
		coordDone <- coordCmd.Wait()
	}()
	defer func() {
		postShutdownAndWait(t, "coordinator", coordBase, coordDone)
	}()
	waitForPing(t, coordBase+"/ping")

	workerCmd := exec.Command(bin,
		"worker",
		"--coordinator", coordBase,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(workerPort),
		"--verbose=false",
	)
	workerCmd.Env = append(os.Environ(), natsURLVar+"="+natsSrv.ClientURL())
	workerCmd.Stdout = os.Stderr
	workerCmd.Stderr = os.Stderr
	if err := workerCmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	workerBase := fmt.Sprintf("http://localhost:%d", workerPort)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- workerCmd.Wait()
	}()
	defer func() {
		postShutdownAndWait(t, "worker", workerBase, workerDone)
	}()
	waitForPing(t, workerBase+"/ping")
	waitForWorkerCount(t, coordBase+"/metrics", 1)

	body, _ := json.Marshal(EvalRequest{Code: "c := Counter new. c increment. c value"})
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(coordBase+"/eval", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("remote worker eval POST failed: %v", err)
	}
	defer resp.Body.Close()
	var er EvalResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("remote worker eval decode failed: %v", err)
	}
	if er.Error != "" {
		t.Fatalf("remote worker eval error: %s", er.Error)
	}
	if got := strings.TrimSpace(er.Output); got != "1" {
		t.Fatalf("remote worker eval output = %q, want 1", got)
	}
}

// freePort returns a currently-free TCP port.  There's an unavoidable race
// between closing the listener and the coordinator binding it, but it's tiny
// and the test is not run in parallel with anything contending for ports.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// waitForPing blocks until url answers 200, or fails the test after ~10s.
func waitForPing(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 100; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("service did not become ready at %s", url)
}

func waitForWorkerCount(t *testing.T, url string, want int) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 100; i++ {
		resp, err := client.Get(url)
		if err == nil {
			var metrics MetricsResponse
			decodeErr := json.NewDecoder(resp.Body).Decode(&metrics)
			resp.Body.Close()
			if decodeErr == nil && metrics.WorkerCount >= want {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("worker count at %s did not reach %d", url, want)
}

func postShutdownAndWait(t *testing.T, name string, base string, done <-chan error) {
	t.Helper()
	resp, err := http.Post(base+"/shutdown", "text/plain", nil)
	if err == nil {
		resp.Body.Close()
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("%s exit after /shutdown: %v", name, err)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("%s did not exit within 10s after /shutdown", name)
	}
}
