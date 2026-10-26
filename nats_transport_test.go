package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

type fakeNATSResponder struct {
	body []byte
}

func (r *fakeNATSResponder) Respond(body []byte) error {
	r.body = body
	return nil
}

type blockingNATSResponder struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingNATSResponder) Respond([]byte) error {
	close(r.started)
	<-r.release
	return nil
}

type signalingNATSResponder struct {
	body []byte
	done chan struct{}
}

func (r *signalingNATSResponder) Respond(body []byte) error {
	r.body = body
	close(r.done)
	return nil
}

func TestNATSTransportDispatchesMethodCall(t *testing.T) {
	withNATSTestServer(t, func() {
		callee := &ObjectServer{
			id:       "callee",
			classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
		}
		if err := callee.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}

		caller := &ObjectServer{id: "caller"}
		caller.registry.Store("callee", ObjectEntry{
			ID:        "callee",
			ClassName: "Thing",
			Subject:   natsSubjectForObject("callee"),
		})

		got := caller.sendMessage(ObjVal("callee"), "class", nil)
		if got.Kind != "string" || got.Str != "Thing" {
			t.Fatalf("NATS call returned %v, want Thing string", got)
		}
	})
}

func TestHTTPObjectDebugGatewayUsesNATS(t *testing.T) {
	withNATSTestServer(t, func() {
		callee := &ObjectServer{
			id:       "callee",
			classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
		}
		if err := callee.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}

		c := &Coordinator{registry: map[string]*ObjectEntry{
			"callee": {ID: "callee", ClassName: "Thing", Subject: natsSubjectForObject("callee")},
		}}

		callBody, _ := json.Marshal(CallRequest{Method: "class"})
		callReq := httptest.NewRequest(http.MethodPost, "/object/callee/call", bytes.NewReader(callBody))
		callRec := httptest.NewRecorder()
		c.handleObjectDebug(callRec, callReq)
		if callRec.Code != http.StatusOK {
			t.Fatalf("call status = %d body=%s", callRec.Code, callRec.Body.String())
		}
		var callResp CallResponse
		if err := json.Unmarshal(callRec.Body.Bytes(), &callResp); err != nil {
			t.Fatal(err)
		}
		if callResp.Result.Kind != "string" || callResp.Result.Str != "Thing" {
			t.Fatalf("call response = %v, want Thing", callResp.Result)
		}

		inspectReq := httptest.NewRequest(http.MethodGet, "/object/callee/inspect", nil)
		inspectRec := httptest.NewRecorder()
		c.handleObjectDebug(inspectRec, inspectReq)
		if inspectRec.Code != http.StatusOK {
			t.Fatalf("inspect status = %d body=%s", inspectRec.Code, inspectRec.Body.String())
		}
		var inspectResp InspectResponse
		if err := json.Unmarshal(inspectRec.Body.Bytes(), &inspectResp); err != nil {
			t.Fatal(err)
		}
		if inspectResp.ObjectID != "callee" || inspectResp.ClassName != "Thing" {
			t.Fatalf("inspect response = %+v", inspectResp)
		}
	})
}

func TestNATSSubjectUsesClusterNamespace(t *testing.T) {
	t.Setenv(natsClusterVar, "dev.cluster 1")
	got := natsSubjectForObject("coordinator")
	want := "smt.dev_cluster_1.obj.coordinator.call"
	if got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestNATSObjectMailboxSerializesConcurrentCalls(t *testing.T) {
	withNATSTestServer(t, func() {
		cd, err := ParseClassSource(`
(class Counter
  (vars count)
  (method initialize () (set count 0))
  (method increment () (set count (send count + 1)))
  (method value () count))
`)
		if err != nil {
			t.Fatal(err)
		}
		callee := &ObjectServer{
			id:           "counter",
			classDef:     cd,
			instanceVars: map[string]Value{"count": Nil},
		}
		if err := callee.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}
		if _, err := sendNATSCall(natsSubjectForObject("counter"), "initialize", nil, 5*time.Second); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := sendNATSCall(natsSubjectForObject("counter"), "increment", nil, 5*time.Second); err != nil {
					t.Errorf("increment: %v", err)
				}
			}()
		}
		wg.Wait()

		reply, err := sendNATSCall(natsSubjectForObject("counter"), "value", nil, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if reply.Result.Kind != "number" || reply.Result.Num != 200 {
			t.Fatalf("counter value = %v, want 200", reply.Result)
		}
	})
}

func TestNATSObjectMailboxAllowsReentrantSelfCall(t *testing.T) {
	withNATSTestServer(t, func() {
		cd, err := ParseClassSource(`
(class Reentrant
  (method outer () (send self inner))
  (method inner () 42))
`)
		if err != nil {
			t.Fatal(err)
		}
		callee := &ObjectServer{
			id:           "reentrant",
			classDef:     cd,
			instanceVars: map[string]Value{},
		}
		callee.registry.Store("reentrant", ObjectEntry{
			ID:        "reentrant",
			ClassName: "Reentrant",
			Subject:   natsSubjectForObject("reentrant"),
		})
		if err := callee.startNATSReceiver(); err != nil {
			t.Fatal(err)
		}

		reply, err := sendNATSCall(natsSubjectForObject("reentrant"), "outer", nil, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if reply.Result.Kind != "number" || reply.Result.Num != 42 {
			t.Fatalf("outer = %v, want 42", reply.Result)
		}
	})
}

func TestNATSObjectMailboxRejectsWhenFull(t *testing.T) {
	obj := &ObjectServer{
		id:             "busy",
		classDef:       &ClassDef{Name: "Busy", Methods: map[string]*MethodDef{}},
		mailbox:        make(chan objectNATSCall, 1),
		controlMailbox: make(chan objectNATSCall, 1),
	}
	obj.mailbox <- objectNATSCall{}
	responder := &fakeNATSResponder{}

	obj.dispatchNATSCall(MessageRequest{MsgID: "m1", Method: "work"}, responder)

	var reply ReplyRequest
	if err := json.Unmarshal(responder.body, &reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Signaled || reply.SignalMsg == "" {
		t.Fatalf("reply = %+v, want mailbox-full signal", reply)
	}
	if obj.mailboxRejected.Load() != 1 {
		t.Fatalf("mailboxRejected = %d, want 1", obj.mailboxRejected.Load())
	}
}

func TestNATSObjectControlMailboxRejectsWhenFull(t *testing.T) {
	obj := &ObjectServer{
		id:             "busy-control",
		classDef:       &ClassDef{Name: "Busy", Methods: map[string]*MethodDef{}},
		mailbox:        make(chan objectNATSCall, 1),
		controlMailbox: make(chan objectNATSCall, 1),
	}
	obj.controlMailbox <- objectNATSCall{}
	responder := &fakeNATSResponder{}

	obj.dispatchNATSCall(MessageRequest{MsgID: "m1", Method: "pulse:"}, responder)

	var reply ReplyRequest
	if err := json.Unmarshal(responder.body, &reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Signaled || !strings.Contains(reply.SignalMsg, "control mailbox full") {
		t.Fatalf("reply = %+v, want control mailbox-full signal", reply)
	}
	if obj.mailboxRejected.Load() != 1 {
		t.Fatalf("mailboxRejected = %d, want 1", obj.mailboxRejected.Load())
	}
}

func TestNATSControlMailboxRunsWhileApplicationReplyIsBlocked(t *testing.T) {
	obj := &ObjectServer{id: "control-priority", classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}}}
	obj.ensureMailbox()
	blocked := &blockingNATSResponder{started: make(chan struct{}), release: make(chan struct{})}
	obj.mailbox <- objectNATSCall{req: MessageRequest{MsgID: "application", Method: "class"}, msg: blocked}
	<-blocked.started

	control := &signalingNATSResponder{done: make(chan struct{})}
	obj.controlMailbox <- objectNATSCall{req: MessageRequest{MsgID: "control", Method: "class"}, msg: control}
	select {
	case <-control.done:
	case <-time.After(time.Second):
	}
	close(blocked.release)
	if len(control.body) == 0 {
		t.Fatal("control call did not complete while an application reply was blocked")
	}
}

func TestMessageCallTimeoutUsesSelectorArgument(t *testing.T) {
	tests := []struct {
		method string
		args   []Value
		want   time.Duration
	}{
		{"ordinary", nil, 30 * time.Second},
		{"getWithTimeout:timeoutMs:", []Value{StrVal("http://localhost"), NumVal(40_000)}, 45 * time.Second},
		{"get:timeoutMs:", []Value{StrVal("http://localhost"), NumVal(1_000)}, 6 * time.Second},
		{"parallelSend:withEach:timeoutMs:", []Value{StrVal("work:"), ObjVal("args"), NumVal(120_000)}, 125 * time.Second},
	}
	for _, test := range tests {
		if got := messageCallTimeout(test.method, test.args); got != test.want {
			t.Errorf("messageCallTimeout(%q) = %s, want %s", test.method, got, test.want)
		}
	}
}

func TestNATSCallRejectsOversizedPayloadBeforeBrokerRequest(t *testing.T) {
	t.Setenv(natsMaxCallBytesVar, "32")
	_, err := sendNATSCall("smt.default.obj.target.call", "echo:", []Value{StrVal(strings.Repeat("x", 64))}, time.Second)
	if err == nil {
		t.Fatal("sendNATSCall accepted oversized payload")
	}
	msg := err.Error()
	for _, want := range []string{"smt.default.obj.target.call.echo:", "maximum is 32", natsMaxCallBytesVar} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want substring %q", msg, want)
		}
	}
}

func TestNATSRecoversAfterServerRestart(t *testing.T) {
	port := freePort(t)
	srv := startNATSTestServerOnPort(t, port)
	defer srv.Shutdown()

	t.Setenv(natsURLVar, "nats://127.0.0.1:"+strconv.Itoa(port))
	activeTransport.Close()

	callee := &ObjectServer{
		id:       "restart-callee",
		classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
	}
	if err := callee.startNATSReceiver(); err != nil {
		t.Fatal(err)
	}
	subject := natsSubjectForObject("restart-callee")
	if _, err := sendNATSCall(subject, "class", nil, 5*time.Second); err != nil {
		t.Fatalf("pre-restart call: %v", err)
	}

	srv.Shutdown()
	srv = startNATSTestServerOnPort(t, port)
	defer srv.Shutdown()

	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		reply, err := sendNATSCall(subject, "class", nil, time.Second)
		if err == nil && reply.Result.Kind == "string" && reply.Result.Str == "Thing" {
			return
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("NATS call did not recover after server restart; last error: %v", lastErr)
}

func BenchmarkNATSTrafficSmallCalls(b *testing.B) {
	withNATSTestServer(b, func() {
		callee := &ObjectServer{
			id:       "bench-callee",
			classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
		}
		if err := callee.startNATSReceiver(); err != nil {
			b.Fatal(err)
		}
		subject := natsSubjectForObject("bench-callee")

		start := time.Now()
		latencies := make([]time.Duration, 0, benchmarkLatencySampleCap(b.N))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			callStart := time.Now()
			reply, err := sendNATSCall(subject, "class", nil, 5*time.Second)
			if len(latencies) < cap(latencies) {
				latencies = append(latencies, time.Since(callStart))
			}
			if err != nil {
				b.Fatal(err)
			}
			if reply.Result.Kind != "string" || reply.Result.Str != "Thing" {
				b.Fatalf("reply = %v, want Thing", reply.Result)
			}
		}
		b.StopTimer()
		reportNATSTrafficMetrics(b, start, b.N, callee)
		reportLatencyMetrics(b, latencies)
	})
}

func BenchmarkNATSTrafficFanout(b *testing.B) {
	withNATSTestServer(b, func() {
		const objectCount = 64
		subjects := make([]string, 0, objectCount)
		for i := 0; i < objectCount; i++ {
			id := "bench-fanout-" + strconv.Itoa(i)
			callee := &ObjectServer{
				id:       id,
				classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
			}
			if err := callee.startNATSReceiver(); err != nil {
				b.Fatal(err)
			}
			subjects = append(subjects, natsSubjectForObject(id))
		}

		var next atomic.Uint64
		start := time.Now()
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				idx := int(next.Add(1)-1) % len(subjects)
				reply, err := sendNATSCall(subjects[idx], "class", nil, 5*time.Second)
				if err != nil {
					b.Fatal(err)
				}
				if reply.Result.Kind != "string" || reply.Result.Str != "Thing" {
					b.Fatalf("reply = %v, want Thing", reply.Result)
				}
			}
		})
		b.StopTimer()
		reportNATSTrafficMetrics(b, start, b.N)
	})
}

func BenchmarkNATSTrafficPulse(b *testing.B) {
	withNATSTestServer(b, func() {
		callee := &ObjectServer{
			id:       "bench-pulse",
			classDef: &ClassDef{Name: "Thing", Methods: map[string]*MethodDef{}},
		}
		if err := callee.startNATSReceiver(); err != nil {
			b.Fatal(err)
		}
		subject := natsSubjectForObject("bench-pulse")

		start := time.Now()
		latencies := make([]time.Duration, 0, benchmarkLatencySampleCap(b.N))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			callStart := time.Now()
			reply, err := sendNATSCall(subject, "pulse:", []Value{NumVal(float64(time.Now().UnixMilli() + int64(i) + 1))}, 5*time.Second)
			if len(latencies) < cap(latencies) {
				latencies = append(latencies, time.Since(callStart))
			}
			if err != nil {
				b.Fatal(err)
			}
			if reply.Signaled {
				b.Fatalf("pulse signaled: %s", reply.SignalMsg)
			}
		}
		b.StopTimer()
		reportNATSTrafficMetrics(b, start, b.N, callee)
		reportLatencyMetrics(b, latencies)
	})
}

func BenchmarkNATSTrafficLargePayload(b *testing.B) {
	withNATSTestServer(b, func() {
		cd, err := ParseClassSource(`
(class Echo
  (method echo: (x) x))
`)
		if err != nil {
			b.Fatal(err)
		}
		callee := &ObjectServer{
			id:           "bench-echo",
			classDef:     cd,
			instanceVars: map[string]Value{},
		}
		if err := callee.startNATSReceiver(); err != nil {
			b.Fatal(err)
		}
		subject := natsSubjectForObject("bench-echo")
		payload := strings.Repeat("x", 64*1024)

		start := time.Now()
		b.SetBytes(int64(len(payload)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reply, err := sendNATSCall(subject, "echo:", []Value{StrVal(payload)}, 5*time.Second)
			if err != nil {
				b.Fatal(err)
			}
			if reply.Result.Kind != "string" || len(reply.Result.Str) != len(payload) {
				b.Fatalf("reply length = %d, want %d", len(reply.Result.Str), len(payload))
			}
		}
		b.StopTimer()
		reportNATSTrafficMetrics(b, start, b.N, callee)
	})
}

func withNATSTestServer(t testing.TB, run func()) {
	t.Helper()
	srv := startNATSTestServerOnPort(t, -1)
	defer srv.Shutdown()

	t.Setenv(natsURLVar, srv.ClientURL())
	activeTransport.Close()
	defer activeTransport.Close()

	run()
}

func startNATSTestServerOnPort(t testing.TB, port int) *natsserver.Server {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Host:   "127.0.0.1",
		Port:   port,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	return srv
}

func benchmarkLatencySampleCap(n int) int {
	if n < 1 {
		return 1
	}
	if n > 100000 {
		return 100000
	}
	return n
}

func reportNATSTrafficMetrics(b *testing.B, start time.Time, calls int, objects ...*ObjectServer) {
	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		b.ReportMetric(float64(calls)/elapsed, "msg_per_s")
	}
	b.ReportMetric(float64(runtime.NumCPU()), "cpus")
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
	if fds := openFDCount(); fds >= 0 {
		b.ReportMetric(float64(fds), "open_fds")
	}
	totalDepth := 0
	totalControlDepth := 0
	for _, obj := range objects {
		if obj == nil || obj.mailbox == nil || obj.controlMailbox == nil {
			continue
		}
		totalDepth += len(obj.mailbox)
		totalControlDepth += len(obj.controlMailbox)
	}
	b.ReportMetric(float64(totalDepth), "mailbox_depth")
	b.ReportMetric(float64(totalControlDepth), "control_mailbox_depth")
}

func reportLatencyMetrics(b *testing.B, samples []time.Duration) {
	if len(samples) == 0 {
		return
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	b.ReportMetric(float64(samples[len(samples)/2].Nanoseconds()), "p50_ns")
	b.ReportMetric(float64(samples[percentileIndex(len(samples), 99)].Nanoseconds()), "p99_ns")
	b.ReportMetric(float64(len(samples)), "latency_samples")
}

func percentileIndex(n int, p int) int {
	idx := (n*p + 99) / 100
	if idx <= 0 {
		return 0
	}
	if idx > n {
		return n - 1
	}
	return idx - 1
}

func openFDCount() int {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}
