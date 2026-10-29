package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nats-io/nats.go"
)

const natsURLVar = "SMT_NATS_URL"
const natsClusterVar = "SMT_CLUSTER"
const natsStartupTimeoutVar = "SMT_NATS_STARTUP_TIMEOUT_MS"
const natsDefaultStartupTimeout = 60 * time.Second
const natsDefaultReconnectWait = time.Second
const mailboxSizeVar = "SMT_MAILBOX_SIZE"
const controlMailboxSizeVar = "SMT_CONTROL_MAILBOX_SIZE"

var natsConn *nats.Conn
var natsConnMu sync.Mutex
var activeTransport objectTransport = natsCoreTransport{}

type objectTransport interface {
	Init() error
	SubjectForObject(id string) string
	Subscribe(subject string, handler func(MessageRequest, natsResponder)) error
	Request(subject string, req MessageRequest, timeout time.Duration) (ReplyRequest, error)
	Close()
}

type natsCoreTransport struct{}

type natsResponder interface {
	Respond([]byte) error
}

func natsURL() string {
	if url := os.Getenv(natsURLVar); url != "" {
		return url
	}
	return nats.DefaultURL
}

func natsClusterName() string {
	name := os.Getenv(natsClusterVar)
	if name == "" {
		name = "default"
	}
	return natsSubjectToken(name)
}

func natsStartupTimeout() time.Duration {
	raw := os.Getenv(natsStartupTimeoutVar)
	if raw == "" {
		return natsDefaultStartupTimeout
	}
	var ms int
	if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil || ms < 0 {
		return natsDefaultStartupTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func natsSubjectToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func natsSubjectForObject(id string) string {
	return activeTransport.SubjectForObject(id)
}

func (natsCoreTransport) SubjectForObject(id string) string {
	return "smt." + natsClusterName() + ".obj." + natsSubjectToken(id) + ".call"
}

func initNATSTransport() error {
	return activeTransport.Init()
}

func (natsCoreTransport) Init() error {
	natsConnMu.Lock()
	defer natsConnMu.Unlock()
	if natsConn != nil {
		return nil
	}
	deadline := time.Now().Add(natsStartupTimeout())
	var lastErr error
	for {
		nc, err := nats.Connect(
			natsURL(),
			nats.Name("servicetalk"),
			nats.Timeout(2*time.Second),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(natsDefaultReconnectWait),
		)
		if err == nil {
			natsConn = nc
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("connect NATS %s within %s: %w", natsURL(), natsStartupTimeout(), lastErr)
		}
		time.Sleep(natsDefaultReconnectWait)
	}
}

func (obj *ObjectServer) startNATSReceiver() error {
	obj.ensureMailbox()
	subject := natsSubjectForObject(obj.id)
	return activeTransport.Subscribe(subject, func(req MessageRequest, msg natsResponder) {
		obj.dispatchNATSCall(req, msg)
	})
}

func (natsCoreTransport) Subscribe(subject string, handler func(MessageRequest, natsResponder)) error {
	if err := initNATSTransport(); err != nil {
		return err
	}
	_, err := natsConn.Subscribe(subject, func(msg *nats.Msg) {
		go func() {
			var req MessageRequest
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				_ = msg.Respond(replyBytes(ReplyRequest{Signaled: true, SignalMsg: "bad NATS message: " + err.Error()}))
				return
			}
			handler(req, msg)
		}()
	})
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", subject, err)
	}
	return natsConn.Flush()
}

func (obj *ObjectServer) dispatchNATSCall(req MessageRequest, msg natsResponder) {
	runtimeCounters.callsIn.Add(1)
	obj.callsReceived.Add(1)
	if obj.serviceConcurrentMethod(req.Method) { go obj.replyToNATSControlCall(req, msg); return } else {}
	if obj.isActiveChain(req.ChainID) {
		obj.replyToNATSCall(req, msg)
		return
	}
	call := objectNATSCall{req: req, msg: msg}
	if isControlMethod(req.Method) || obj.processControlMethod(req.Method) {
		select {
		case obj.controlMailbox <- call:
		default:
			obj.rejectNATSCall(req, msg, "control mailbox full")
		}
		return
	}
	select {
	case obj.mailbox <- call:
	default:
		obj.rejectNATSCall(req, msg, "mailbox full")
	}
}

func (obj *ObjectServer) ensureMailbox() {
	obj.mailboxOnce.Do(func() {
		obj.mailbox = make(chan objectNATSCall, positiveEnvInt(mailboxSizeVar, 1024))
		obj.controlMailbox = make(chan objectNATSCall, positiveEnvInt(controlMailboxSizeVar, 128))
		go obj.runMailbox()
		go obj.runControlMailbox()
	})
}

func positiveEnvInt(name string, fallback int) int {
	n := envInt(name, fallback)
	if n <= 0 {
		return fallback
	}
	return n
}

func (obj *ObjectServer) runMailbox() {
	for call := range obj.mailbox {
		obj.replyToNATSCall(call.req, call.msg)
	}
}

func (obj *ObjectServer) runControlMailbox() {
	for call := range obj.controlMailbox {
		obj.replyToNATSControlCall(call.req, call.msg)
	}
}

func (obj *ObjectServer) replyToNATSControlCall(req MessageRequest, msg natsResponder) {
	reply := dispatchToReply(req.MsgID, func() Value {
		return obj.callMethod(req.Method, req.Args)
	})
	obj.callsCompleted.Add(1)
	_ = msg.Respond(replyBytes(reply))
}

func (obj *ObjectServer) replyToNATSCall(req MessageRequest, msg natsResponder) {
	traceGeneration := obj.trace.begin(obj.id, req)
	finish := obj.trackCall(req.MsgID, req.ChainID, req.Method, obj.id, "executing")
	defer finish()
	prev := obj.setActiveChain(req.ChainID)
	defer obj.restoreActiveChain(prev)
	reply := dispatchToReply(req.MsgID, func() Value {
		return obj.callMethod(req.Method, req.Args)
	})
	obj.callsCompleted.Add(1)
	obj.trace.finish(traceGeneration, reply)
	_ = msg.Respond(replyBytes(reply))
}

func (obj *ObjectServer) rejectNATSCall(req MessageRequest, msg natsResponder, reason string) {
	traceGeneration := obj.trace.begin(obj.id, req)
	obj.trace.finish(traceGeneration, ReplyRequest{MsgID: req.MsgID, Signaled: true, SignalMsg: reason, Result: Nil})
	runtimeCounters.queueFull.Add(1)
	obj.mailboxRejected.Add(1)
	_ = msg.Respond(replyBytes(ReplyRequest{
		MsgID:     req.MsgID,
		Signaled:  true,
		SignalMsg: reason + " on " + obj.id + " while handling " + req.Method,
	}))
}

func isControlMethod(method string) bool {
	switch method {
	case "pulse:", "class", "__inspectJson", "__metricsJson", "__debugStart:", "__debugCommand:", "__debugJson", "__trace:", "__traceJson", "__nativeSnapshotJson", "__controlState", "__controlOwner", "__controlOwner:", "__controlConfigure:owner:children:", "__forceShutdown":
		return true
	default:
		return false
	}
}

func (obj *ObjectServer) isActiveChain(chainID string) bool {
	if chainID == "" {
		return false
	}
	obj.activeChainMu.RLock()
	ok := obj.activeChainID == chainID
	obj.activeChainMu.RUnlock()
	return ok
}

func (obj *ObjectServer) setActiveChain(chainID string) string {
	obj.activeChainMu.Lock()
	prev := obj.activeChainID
	obj.activeChainID = chainID
	obj.activeChainMu.Unlock()
	return prev
}

func (obj *ObjectServer) restoreActiveChain(chainID string) {
	obj.activeChainMu.Lock()
	obj.activeChainID = chainID
	obj.activeChainMu.Unlock()
}

func (obj *ObjectServer) currentChainID() string {
	obj.activeChainMu.RLock()
	chainID := obj.activeChainID
	obj.activeChainMu.RUnlock()
	return chainID
}

func (c *Coordinator) startNATSReceiver() error {
	subject := natsSubjectForObject(coordinatorID)
	return activeTransport.Subscribe(subject, func(req MessageRequest, msg natsResponder) {
		runtimeCounters.callsIn.Add(1)
		traceGeneration := c.trace.begin(coordinatorID, req)
		reply := dispatchToReply(req.MsgID, func() Value {
			return c.dispatchCoordinatorMethod(req.Method, req.Args)
		})
		c.trace.finish(traceGeneration, reply)
		_ = msg.Respond(replyBytes(reply))
	})
}

func sendNATSCall(subject string, method string, args []Value, timeout time.Duration) (ReplyRequest, error) {
	return sendNATSCallWithChain(subject, method, args, timeout, "")
}

func sendNATSCallWithChain(subject string, method string, args []Value, timeout time.Duration, chainID string) (ReplyRequest, error) {
	return sendNATSCallFrom(subject, method, args, timeout, chainID, "")
}

func sendNATSCallFrom(subject string, method string, args []Value, timeout time.Duration, chainID, sender string) (ReplyRequest, error) {
	if subject == "" {
		return ReplyRequest{}, fmt.Errorf("missing NATS subject")
	}
	msgID := newUUID()
	if chainID == "" {
		chainID = msgID
	}
	req := MessageRequest{
		Sender: sender,
		MsgID:   msgID,
		ChainID: chainID,
		Method:  method,
		Args:    args,
	}
	return activeTransport.Request(subject, req, timeout)
}

func (natsCoreTransport) Request(subject string, req MessageRequest, timeout time.Duration) (ReplyRequest, error) {
	body, _ := json.Marshal(req)
	maxCallBytes := natsMaxCallBytes()
	if len(body) > maxCallBytes {
		return ReplyRequest{}, fmt.Errorf("NATS call %s.%s encoded to %d bytes, maximum is %d; raise %s and the NATS server max_payload together", subject, req.Method, len(body), maxCallBytes, natsMaxCallBytesVar)
	}
	if err := initNATSTransport(); err != nil {
		return ReplyRequest{}, err
	}
	runtimeCounters.callsOut.Add(1)
	msg, err := natsConn.Request(subject, body, timeout)
	if err != nil {
		recordNATSError(err)
		return ReplyRequest{}, err
	}
	var reply ReplyRequest
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return ReplyRequest{}, err
	}
	return reply, nil
}

func (natsCoreTransport) Close() {
	natsConnMu.Lock()
	defer natsConnMu.Unlock()
	if natsConn != nil {
		natsConn.Close()
		natsConn = nil
	}
}

func (obj *ObjectServer) sendMessageNATS(entry ObjectEntry, method string, args []Value) Value {
	return obj.sendMessageNATSWithTimeout(entry, method, args, messageCallTimeout(method, args))
}

func (obj *ObjectServer) sendMessageNATSWithTimeout(entry ObjectEntry, method string, args []Value, timeout time.Duration) Value {
	finish := obj.trackCall(newUUID(), obj.currentChainID(), method, entry.ID, "waiting")
	defer finish()
	reply, err := sendNATSCallFrom(entry.Subject, method, args, timeout, obj.currentChainID(), obj.id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] sendMessage NATS error: %v\n", obj.id, err)
		panic(signalError{msg: fmt.Sprintf("send %s to object %s: %v", method, entry.ID, err)})
	}
	if reply.Signaled {
		panic(signalError{msg: reply.SignalMsg})
	}
	return reply.Result
}

func messageCallTimeout(method string, args []Value) time.Duration {
	const defaultTimeout = 30 * time.Second
	const responseOverhead = 5 * time.Second
	argumentIndex := -1
	switch method {
	case "prepareClass:", "prepareSource:":
		return spawnTimeout() + defaultTimeout + responseOverhead
	case "call:arguments:", "check:selector:arguments:expect:":
		return defaultTimeout + responseOverhead
	case "await:", "awaitPromise:":
		argumentIndex = 0
	case "parallelSend:withEach:timeoutMs:", "call:arguments:timeoutMs:":
		argumentIndex = 2
	case "getWithTimeout:timeoutMs:", "get:timeoutMs:":
		argumentIndex = 1
	}
	if argumentIndex < 0 || len(args) <= argumentIndex {
		return defaultTimeout
	}
	timeout, ok := messageTimeoutFromMilliseconds(args[argumentIndex])
	if !ok {
		return defaultTimeout
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if timeout > maxDuration-responseOverhead {
		return maxDuration
	}
	return timeout + responseOverhead
}

func messageTimeoutFromMilliseconds(value Value) (time.Duration, bool) {
	const maxDurationMilliseconds = float64((1<<63 - 1) / int64(time.Millisecond))
	if value.Kind != "number" || value.Num <= 0 || math.IsNaN(value.Num) || math.IsInf(value.Num, 0) || value.Num > maxDurationMilliseconds {
		return 0, false
	}
	return time.Duration(value.Num * float64(time.Millisecond)), true
}

func replyBytes(reply ReplyRequest) []byte {
	body, _ := json.Marshal(reply)
	return body
}
