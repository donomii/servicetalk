package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// rpcClient is the shared HTTP client for all internal peer-to-peer calls.
// Its 30 s timeout matches the channel-select timeout in sendMessage and
// prevents goroutine leaks when a peer is dead or slow.
var rpcClient = &http.Client{Timeout: 30 * time.Second}

type ObjectServer struct {
	id             string
	coordinatorURL string
	classDef       *ClassDef
	prototypeID    string

	// Block objects: non-nil blockDef, no classDef methods
	blockDef *BlockDef

	// Array objects: Go-backed slice
	arrayData []Value

	// Dictionary objects: Go-backed map
	dictionaryData map[string]Value

	// Foreign (C-backed) objects: subprocess bridge.  When set, method
	// calls are routed through this instead of runBody.
	foreignBridge *ForeignBridge

	// HttpServer state (only set for __httpserver__ instances).
	httpServerState *httpServerState

	// RobotsMatcher state (only set for __robotsmatcher__ instances).
	robotsState *robotsState

	// SitemapParser state (only set for __sitemapparser__ instances).
	sitemapState *sitemapParserState

	// Process state (only set for __process__ instances).
	procState *processState
	procStateOnce sync.Once

	// Instance variables (regular objects + workspace state)
	mu           sync.RWMutex
	instanceVars map[string]Value
	promiseDone chan struct{}

	// Pulse-based GC.
	// lastSweepID is the most recent sweep id this object has accepted —
	// time.Now().UnixMilli() at whichever root started the sweep.  An
	// incoming pulse with SweepID <= lastSweepID is dropped (cycle break).
	// pulseTimeout is how long an object will tolerate going un-pulsed
	// before deregistering and exiting.  Initialised to spawn time so a
	// freshly-spawned object isn't killed before its creator's first
	// pulse can reach it.
	lastSweepID  int64 // atomic
	pulseTimeout time.Duration
	pulseClockMu sync.Mutex
	lastPulseAt time.Time

	registry sync.Map // objectID -> ObjectEntry
	pending  sync.Map // msgID -> chan ReplyRequest

	mailboxOnce     sync.Once
	mailbox         chan objectNATSCall
	controlMailbox  chan objectNATSCall
	activeChainMu   sync.RWMutex
	activeChainID   string
	callsReceived   atomic.Uint64
	callsCompleted  atomic.Uint64
	mailboxRejected atomic.Uint64
	activityMu      sync.Mutex
	activity        map[string]CallActivity
	debugMu         sync.Mutex
	debug           *debugSession
	trace           traceBuffer

	// Workspace: output capture during eval: calls
	evalCapture   []string
	evalCaptureMu sync.Mutex
	evalCapturing bool
	evalSerial    sync.Mutex // serialises concurrent eval: calls
}

type objectNATSCall struct {
	req MessageRequest
	msg natsResponder
}

func runObject(className, id string, coordinatorURL string, autoInit bool, defFile string, pulseTimeoutSecs int, snapshotFiles ...string) {
	obj := &ObjectServer{
		id:             id,
		coordinatorURL: coordinatorURL,
		instanceVars:   make(map[string]Value),
		lastPulseAt:    time.Now(),
		pulseTimeout:   time.Duration(pulseTimeoutSecs) * time.Second,
	}

	initializationClass := className
	if len(snapshotFiles) > 0 && snapshotFiles[0] != "" { initializationClass = snapshotInitClass } else {}
	switch initializationClass {
	case snapshotInitClass:
		if err := obj.restoreNativeSnapshot(snapshotFiles[0]); err != nil {
			body, _ := json.Marshal(map[string]string{"objectId": id, "error": err.Error()})
			rpcClient.Post(coordinatorURL+"/spawn-failed", "application/json", bytes.NewReader(body))
			fmt.Fprintf(os.Stderr, "[%s] clone snapshot: %v\n", id, err)
			os.Exit(1)
		} else {}
	case BuiltinClassBlock:
		if defFile == "" {
			fmt.Fprintf(os.Stderr, "[%s] block object requires --def\n", id)
			os.Exit(1)
		}
		data, err := os.ReadFile(defFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] cannot read block def %s: %v\n", id, defFile, err)
			os.Exit(1)
		}
		os.Remove(defFile) // temp file — clean up immediately
		var bd BlockDef
		if err := json.Unmarshal(data, &bd); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] cannot parse block def: %v\n", id, err)
			os.Exit(1)
		}
		obj.blockDef = &bd
		obj.classDef = &ClassDef{Name: BuiltinClassBlock, Methods: make(map[string]*MethodDef)}

	case BuiltinClassArray:
		obj.classDef = &ClassDef{Name: BuiltinClassArray, Methods: make(map[string]*MethodDef)}
		obj.arrayData = make([]Value, 0)

	case BuiltinClassDictionary:
		obj.classDef = &ClassDef{Name: BuiltinClassDictionary, Methods: make(map[string]*MethodDef)}
		obj.dictionaryData = make(map[string]Value)

	case BuiltinClassHttpClient:
		obj.classDef = &ClassDef{Name: BuiltinClassHttpClient, Methods: make(map[string]*MethodDef)}

	case BuiltinClassHttpServer:
		obj.classDef = &ClassDef{Name: BuiltinClassHttpServer, Methods: make(map[string]*MethodDef)}

	case BuiltinClassRobotsMatcher:
		obj.classDef = &ClassDef{Name: BuiltinClassRobotsMatcher, Methods: make(map[string]*MethodDef)}

	case BuiltinClassSitemapParser:
		obj.classDef = &ClassDef{Name: BuiltinClassSitemapParser, Methods: make(map[string]*MethodDef)}

	case BuiltinClassProcess:
		obj.classDef = &ClassDef{Name: BuiltinClassProcess, Methods: make(map[string]*MethodDef)}

	case BuiltinClassWorkspace:
		// Persistent REPL object — no class file, no special data.
		// State lives in instanceVars; idle timeout is 0 (never exits).
		obj.classDef = &ClassDef{Name: BuiltinClassWorkspace, Methods: make(map[string]*MethodDef)}

	default:
		cd, err := LoadClass(className)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] %v\n", id, err)
			body, _ := json.Marshal(map[string]string{"objectId": id, "error": err.Error()})
			rpcClient.Post(coordinatorURL+"/spawn-failed", "application/json", bytes.NewReader(body))
			os.Exit(1)
		}
		obj.classDef = cd
		obj.instanceVars = initialInstanceVars(cd)
		if cd.Foreign != nil {
			bridge, err := NewForeignBridge(obj, cd.Foreign)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] foreign bridge: %v\n", id, err)
				body, _ := json.Marshal(map[string]string{"objectId": id, "error": err.Error()})
				rpcClient.Post(coordinatorURL+"/spawn-failed", "application/json", bytes.NewReader(body))
				os.Exit(1)
			}
			obj.foreignBridge = bridge
		}
	}

	if err := obj.startNATSReceiver(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] NATS receiver: %v\n", id, err)
		body, _ := json.Marshal(map[string]string{"objectId": id, "error": err.Error()})
		rpcClient.Post(coordinatorURL+"/spawn-failed", "application/json", bytes.NewReader(body))
		os.Exit(1)
	}
	obj.registry.Store(id, ObjectEntry{ID: id, Subject: natsSubjectForObject(id), ClassName: obj.classDef.Name})

	// Run initialize BEFORE registering so external callers can't reach
	// the object until its instance vars are set up.  Otherwise a spawn-
	// then-immediately-call sequence (e.g. `smt shell` → spawn Shell →
	// shellCwdHint sends `pwd`) races with initialize and the first
	// few calls see nil instance vars.
	if autoInit {
		if _, _, has := lookupMethod(obj.classDef, "initialize"); has {
			obj.callMethod("initialize", nil)
		}
	}

	if err := obj.register(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] register failed: %v\n", obj.id, err)
		body, _ := json.Marshal(map[string]string{"objectId": id, "error": err.Error()})
		rpcClient.Post(coordinatorURL+"/spawn-failed", "application/json", bytes.NewReader(body))
		os.Exit(1)
	}

	if obj.pulseTimeout > 0 {
		go obj.pulseWatchLoop()
	}

	logInfo("[%s] %s ready on %s\n", id, className, natsSubjectForObject(id))

	select {}
}

// ---- Lifecycle ----

func (obj *ObjectServer) register() error {
	req := RegisterRequest{
		ObjectID:  obj.id,
		ClassName: obj.classDef.Name,
		Subject:   natsSubjectForObject(obj.id),
	}
	body, _ := json.Marshal(req)
	deadline := time.Now().Add(spawnTimeout())
	var lastErr error
	for {
		resp, err := rpcClient.Post(obj.coordinatorURL+"/register", "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("register status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("register with coordinator %s within %s: %w", obj.coordinatorURL, spawnTimeout(), lastErr)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (obj *ObjectServer) deregister() {
	req := DeregisterRequest{ObjectID: obj.id}
	body, _ := json.Marshal(req)
	resp, err := rpcClient.Post(obj.coordinatorURL+"/deregister", "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (obj *ObjectServer) shutdownAndExit() {
	if obj.foreignBridge != nil {
		obj.foreignBridge.Close()
	}
	obj.deregister()
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

// fetchRegistry refreshes the local registry cache from the
// coordinator.  Called only on registry-lookup miss — objects do
// not periodically hit the coordinator for liveness; that's the
// pulse-GC mechanism's job (coordinator → roots → propagation).
func (obj *ObjectServer) fetchRegistry() {
	resp, err := rpcClient.Get(obj.coordinatorURL + "/registry")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var hb HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&hb); err != nil {
		logInfo("[%s] fetchRegistry decode error: %v\n", obj.id, err)
		return
	}
	for _, entry := range hb.Registry {
		obj.registry.Store(entry.ID, entry)
	}
}

// pulseWatchLoop deregisters and exits when no pulse has reached this
// object for longer than pulseTimeout.  Replaces the old idle-time
// approach with reachability-from-a-root semantics.
func (obj *ObjectServer) pulseWatchLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		since := obj.pulseAge(time.Now())
		if since >= obj.pulseTimeout {
			logInfo("[%s] no pulse for %v, shutting down\n", obj.id, since.Round(time.Second))
			if obj.foreignBridge != nil {
				obj.foreignBridge.Close()
			}
			obj.deregister()
			os.Exit(0)
		}
	}
}

// observePulse atomically updates lastSweepID iff the incoming sweepID
// is strictly greater than what we've already seen.  Returns true when
// the update happened — callers use that to decide whether to propagate
// the pulse onward (which is what makes cycles terminate: A → B → A
// loops in on the same sweepID and the second pass returns false).
func (obj *ObjectServer) observePulse(sweepID int64) bool {
	for {
		last := atomic.LoadInt64(&obj.lastSweepID)
		if sweepID <= last {
			return false
		}
		if atomic.CompareAndSwapInt64(&obj.lastSweepID, last, sweepID) {
			obj.recordPulseArrival(time.Now())
			return true
		}
	}
}

// handlePulseEndpoint is retained for old debug clients; normal pulse
// traffic is NATS pulse: dispatch.
func (obj *ObjectServer) handlePulseEndpoint(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req PulseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad pulse request: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	go obj.callMethod("pulse:", []Value{NumVal(float64(req.SweepID))})
}

// dispatchPulse runs whatever pulse: handler is appropriate for this
// object: a user override (ST or foreign) if the class declares one,
// otherwise the default walk-and-forward over instanceVars / arrayData
// / dictionaryData / blockDef.CapturedArgs.
func (obj *ObjectServer) dispatchPulse(args []Value) Value {
	if len(args) < 1 || args[0].Kind != "number" {
		return Nil
	}
	sweepID := int64(args[0].Num)
	if !obj.observePulse(sweepID) {
		return Nil
	}

	// User override (ST class with (method pulse: ...) or foreign class
	// with (cmethod pulse: ...) ).  parseClassNode and parseForeignClassNode
	// both populate Methods["pulse:"] when the user declared one.
	if method, ok := obj.classDef.Methods["pulse:"]; ok {
		if obj.foreignBridge != nil {
			return obj.dispatchForeign("pulse:", args)
		}
		argMap := map[string]Value{}
		for i, p := range method.Params {
			if i < len(args) {
				argMap[p] = args[i]
			} else {
				argMap[p] = Nil
			}
		}
		return obj.runBody(&EvalCtx{obj: obj, args: argMap}, method.Body)
	}

	obj.defaultPulse(sweepID)
	return Nil
}

// defaultPulse walks every state slot the runtime can see and forwards
// the pulse to every objid value it finds.  Foreign classes have no
// Go-side state, so this is a no-op for them — they must define
// (cmethod pulse: ...) themselves if they hold object refs in C.
func (obj *ObjectServer) defaultPulse(sweepID int64) {
	refs := obj.collectObjidRefs()
	for _, id := range refs {
		go obj.forwardPulse(id, sweepID)
	}
}

// collectObjidRefs returns every objid this object holds in any of the
// runtime-visible slots.  Order doesn't matter; duplicates are fine.
func (obj *ObjectServer) collectObjidRefs() []string {
	var refs []string
	obj.mu.RLock()
	for _, v := range obj.instanceVars {
		if v.Kind == "objid" {
			refs = append(refs, v.ObjID)
		}
	}
	for _, v := range obj.arrayData {
		if v.Kind == "objid" {
			refs = append(refs, v.ObjID)
		}
	}
	for _, v := range obj.dictionaryData {
		if v.Kind == "objid" {
			refs = append(refs, v.ObjID)
		}
	}
	obj.mu.RUnlock()
	if obj.blockDef != nil {
		if value := obj.blockDef.SelfOverride; value != nil && value.Kind == "objid" {
			refs = append(refs, value.ObjID)
		} else {
			// Blocks without a bound receiver retain their lexical self.
		}
		if obj.blockDef.ParentID != "" {
			refs = append(refs, obj.blockDef.ParentID)
		}
		for _, v := range obj.blockDef.CapturedArgs {
			if v.Kind == "objid" {
				refs = append(refs, v.ObjID)
			}
		}
	}
	return append(refs, obj.debugReferences()...)
}

// forwardPulse fire-and-forget sends pulse: to a downstream object.
// Errors are silent: a dead target is its own problem and shouldn't
// stall the sweep.
func (obj *ObjectServer) forwardPulse(targetID string, sweepID int64) {
	entry, ok := obj.lookupRegistry(targetID)
	if !ok {
		return
	}
	_, _ = sendNATSCall(entry.Subject, "pulse:", []Value{NumVal(float64(sweepID))}, 3*time.Second)
}

func (obj *ObjectServer) lookupRegistry(id string) (ObjectEntry, bool) {
	if v, ok := obj.registry.Load(id); ok {
		return v.(ObjectEntry), true
	}
	obj.fetchRegistry()
	if v, ok := obj.registry.Load(id); ok {
		return v.(ObjectEntry), true
	}
	return ObjectEntry{}, false
}

// ---- HTTP handlers ----

func (obj *ObjectServer) handleMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad object message request: "+err.Error(), http.StatusBadRequest)
		return
	}
	reply := dispatchToReply(req.MsgID, func() Value {
		return obj.callMethod(req.Method, req.Args)
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

// dispatchToReply runs call() and packages its result into a ReplyRequest.
// A signalError that escapes call() (e.g. ensure: re-raising after cleanup,
// or a bare `signal` send) is encoded into the reply so the caller can
// re-raise it locally — instead of an unrecovered panic killing this
// handler goroutine (and hanging the caller until its send times out).
// Non-signal panics are re-panicked, preserving the fail-fast behaviour for
// genuine runtime bugs.
func dispatchToReply(msgID string, call func() Value) (reply ReplyRequest) {
	reply.MsgID = msgID
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(signalError); ok {
				reply.Signaled = true
				reply.SignalMsg = sig.msg
				return
			}
			panic(r)
		}
	}()
	reply.Result = call()
	return reply
}

func (obj *ObjectServer) handleReply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req ReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad object reply request: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	if ch, ok := obj.pending.Load(req.MsgID); ok {
		ch.(chan ReplyRequest) <- req
	}
}

// ---- Method dispatch ----

// callMethod dispatches an incoming Smalltalk message on this object.
func (obj *ObjectServer) callMethod(name string, args []Value) Value {
	// pulse: is special-cased before the type-specific dispatchers below
	// so it works the same on regular classes, blocks, arrays, dicts,
	// the workspace, and foreign classes.  The dispatch routes through
	// a user override if one is defined, else the default field walker.
	if name == "pulse:" {
		return obj.dispatchPulse(args)
	}

	// perform: — reflective dispatch.  `obj perform: #foo` is `obj foo`;
	// `obj perform: #at:put: with: k with: v` is `obj at: k put: v`;
	// `obj perform: sel withArguments: anArray` reads the args out of the
	// Array first.  Re-enters callMethod so the resolved selector goes
	// through the full dispatch (user methods, built-ins, doesNotUnderstand:).
	switch name {
	case "perform:", "perform:with:", "perform:with:with:", "perform:with:with:with:":
		if len(args) < 1 {
			return Nil
		}
		return obj.callMethod(args[0].String(), args[1:])
	case "perform:withArguments:":
		if len(args) < 1 {
			return Nil
		}
		var callArgs []Value
		if len(args) >= 2 && args[1].Kind == "objid" {
			callArgs = obj.readArrayElements(args[1])
		}
		return obj.callMethod(args[0].String(), callArgs)
	}

	// Universal methods available on every object
	switch name {
	case "class":
		return StrVal(obj.classDef.Name)
	case "yourself":
		return ObjVal(obj.id)
	case "clone", "cloneWith:":
		return obj.cloneDispatch(name, args)
	case "startPromise:method:arguments:timeoutMs:", "promiseResult", "awaitPromise:":
		return obj.promiseDispatch(name, args)
	case "workbenchReferences:":
		return obj.workbenchReferences(args)
	case "__shell:arguments:":
		return obj.shellDispatch(args)
	case "__replaceView:", "__describeView", "__viewRoot", "__viewRoot:", "__controlState", "__controlOwner", "__controlOwner:", "__describeControl", "__controlConfigure:owner:children:", "__controlChildren:", "__controlAdd:", "__controlRemove:":
		return obj.controlDispatch(name, args)
	case "validateWiring:", "retainWiring:in:", "executeWiring:retain:", "executeWiring:retain:previous:":
		return obj.wiringDispatch(name, args)
	case "=":
		if len(args) < 1 {
			return False
		}
		return BoolVal(args[0].Kind == "objid" && args[0].ObjID == obj.id)
	case "~=", "!=":
		if len(args) < 1 {
			return True
		}
		return BoolVal(!(args[0].Kind == "objid" && args[0].ObjID == obj.id))
	case "signal":
		// Any object can signal; the handler receives the printString form.
		panic(signalError{msg: "a " + obj.classDef.Name})
	case "isNil":
		return False
	case "notNil":
		return True
	case "inspect":
		// Print instance variables to transcript and return self.
		obj.mu.RLock()
		vars := make(map[string]Value, len(obj.instanceVars))
		for k, v := range obj.instanceVars {
			vars[k] = v
		}
		obj.mu.RUnlock()
		obj.transcriptf("--- %s (%s) ---", obj.classDef.Name, obj.id)
		for k, v := range vars {
			obj.transcriptf("  %s: %s", k, v.String())
		}
		if len(vars) == 0 {
			obj.transcriptf("  (no instance variables)")
		}
		return ObjVal(obj.id)
	case "__capture:":
		if len(args) < 1 {
			return Nil
		}
		obj.evalCaptureMu.Lock()
		if obj.evalCapturing {
			obj.evalCapture = append(obj.evalCapture, args[0].String())
		}
		obj.evalCaptureMu.Unlock()
		return Nil
	case "__inspectJson":
		body, _ := json.Marshal(obj.inspectResponse())
		return StrVal(string(body))
	case "__metricsJson":
		body, _ := json.Marshal(obj.metricsResponse())
		return StrVal(string(body))
	case "__debugStart:", "__debugCommand:", "__debugJson", "__debugRun:":
		return obj.debugDispatch(name, args)
	case "__nativeSnapshotJson":
		snapshot, err := obj.nativeSnapshot()
		if err != nil { panic(signalError{msg: err.Error()}) } else {}
		body, _ := json.Marshal(snapshot)
		return StrVal(string(body))
	case "__trace:", "__traceJson":
		return obj.trace.dispatch(name, args)
	case "__shutdown":
		obj.shutdownAndExit()
		return Nil
	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		sel := args[0].String()
		switch sel {
		case "class", "yourself", "isNil", "notNil", "printString", "respondsTo:", "getVar:", "setVar:to:":
			return True
		default:
			for _, supported := range obj.inspectionMethodNames() {
				if supported == sel { return True } else {}
			}
			return False
		}

	// Used by blocks to reach parent vars — keep these accessible on all objects
	case "getVar:":
		if len(args) > 0 && args[0].Kind == "string" {
			obj.mu.RLock()
			v := obj.instanceVars[args[0].Str]
			obj.mu.RUnlock()
			return v
		}
		return Nil
	case "setVar:to:":
		if len(args) >= 2 && args[0].Kind == "string" {
			fieldName := args[0].Str
			if !classAllowsSetVar(obj.classDef, fieldName) {
				obj.transcriptf("set: undeclared field %q on class %s; declared fields: %v — add %q to the class's vars list",
					fieldName, obj.classDef.Name, obj.classDef.Vars, fieldName)
				return Nil
			}
			obj.mu.Lock()
			obj.instanceVars[fieldName] = args[1]
			obj.mu.Unlock()
			return args[1]
		}
		return Nil
	}

	// Block objects handle only value/value:/free
	if obj.blockDef != nil {
		if name == "printString" {
			return StrVal("a Block")
		}
		return obj.callBlockMethod(name, args)
	}

	// printString fallback for built-ins that don't override it —
	// everything except Array and Dictionary uses the universal
	// "a HttpClient" / "a Process" / "a Workspace" form.  Array and
	// Dictionary fall through; their dispatchers below format
	// content-aware printString output.
	if name == "printString" && obj.classDef != nil {
		switch obj.classDef.Name {
		case BuiltinClassArray, BuiltinClassDictionary, BuiltinClassProcess:
			// content-aware override is in the dispatcher below
		default:
			// User-defined printString in a user class? Let it through
			// to the method lookup section so the user wins.
			if _, userHasIt := obj.classDef.Methods["printString"]; !userHasIt {
				return StrVal(articleFor(obj.classDef.Name))
			}
		}
	}

	// Built-in classes — see builtins.go for the constants.
	switch obj.classDef.Name {
	case BuiltinClassArray:
		return obj.callArrayMethod(name, args)
	case BuiltinClassDictionary:
		return obj.callDictionaryMethod(name, args)
	case BuiltinClassHttpClient:
		return obj.callHttpClientMethod(name, args)
	case BuiltinClassHttpServer:
		return obj.callHttpServerMethod(name, args)
	case BuiltinClassRobotsMatcher:
		return obj.callRobotsMatcherMethod(name, args)
	case BuiltinClassSitemapParser:
		return obj.callSitemapParserMethod(name, args)
	case BuiltinClassProcess:
		return obj.callProcessMethod(name, args)
	case BuiltinClassWorkspace:
		return obj.callWorkspaceMethod(name, args)
	}

	// Foreign (C-backed) class: route through the subprocess bridge.
	// Catch signalError here the same way runBody does for native classes.
	if obj.foreignBridge != nil {
		return obj.dispatchForeign(name, args)
	}

	// Regular class method lookup — walks the superclass chain.
	method, definingClass, ok := lookupMethod(obj.classDef, name)
	if !ok {
		// printString fallback for user classes that didn't define it —
		// Smalltalk-flavoured "a Counter" rather than the bare objid
		// the REPL would otherwise print.  Built-in classes that want
		// content-aware printing handle the case in their own callX
		// dispatcher.
		if name == "printString" {
			return StrVal(articleFor(obj.classDef.Name))
		}
		// doesNotUnderstand: — try calling it on self first (chain-walked)
		if dnu, dnuClass, hasDNU := lookupMethod(obj.classDef, "doesNotUnderstand:"); hasDNU {
			ctx := &EvalCtx{obj: obj, args: map[string]Value{}, definingClass: dnuClass}
			if len(dnu.Params) > 0 {
				ctx.args[dnu.Params[0]] = StrVal(name)
			}
			return obj.runBody(ctx, dnu.Body)
		}
		if name == "initialize" && len(args) == 0 {
			return ObjVal(obj.id)
		} else {
			return unsupportedMessage(ObjVal(obj.id), obj.classDef.Name, name, args)
		}
	}

	return obj.runMethod(method, definingClass, args)
}

// runMethod binds args to params and evaluates a method body with the
// defining class recorded in the context, so `super` sends inside the
// body can resume lookup from the right point in the chain.
func (obj *ObjectServer) runMethod(method *MethodDef, definingClass *ClassDef, args []Value) Value {
	argMap := make(map[string]Value, len(method.Params))
	for i, p := range method.Params {
		if i < len(args) {
			argMap[p] = args[i]
		} else {
			argMap[p] = Nil
		}
	}
	ctx := &EvalCtx{obj: obj, args: argMap, definingClass: definingClass}
	return obj.runBody(ctx, method.Body)
}

// callMethodFrom dispatches a method looked up starting from `start`
// (rather than the receiver's own class), in-process on this object.
// It backs `super` sends: self and instance variables stay bound to the
// receiver, but lookup begins above the class that defined the calling
// method.  Not found → the receiver's doesNotUnderstand: / transcript.
func (obj *ObjectServer) callMethodFrom(start *ClassDef, name string, args []Value) Value {
	method, definingClass, ok := lookupMethod(start, name)
	if ok {
		return obj.runMethod(method, definingClass, args)
	}
	// Not found above `start`.  Send doesNotUnderstand: to the receiver if
	// it has one (chain-walked), else log.  We do NOT restart normal
	// dispatch from obj.classDef — that would re-find the overriding child
	// method and could recurse.
	if dnu, dnuClass, hasDNU := lookupMethod(obj.classDef, "doesNotUnderstand:"); hasDNU {
		dnuArgs := []Value{}
		if len(dnu.Params) > 0 {
			dnuArgs = append(dnuArgs, StrVal(name))
		}
		return obj.runMethod(dnu, dnuClass, dnuArgs)
	}
	if name == "initialize" && len(args) == 0 {
		return ObjVal(obj.id)
	} else {
		return unsupportedMessage(ObjVal(obj.id), obj.classDef.Name+" super", name, args)
	}
}

// dispatchForeign routes a call to the foreign bridge and recovers from
// signalError the same way runBody does for native classes.
func (obj *ObjectServer) dispatchForeign(name string, args []Value) (result Value) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(signalError); ok {
				panic(sig)
			}
			panic(r)
		}
	}()
	return obj.foreignBridge.dispatch(name, args)
}

// runBody evaluates a sequence of expressions, returning the last value.
// Catches returnSignal (early return) and unhandled signalError (logs, returns nil).
// Uncaught signals now propagate; only the interactive workspace formats them.
func (obj *ObjectServer) runBody(ctx *EvalCtx, body []*Node) (result Value) {
	defer func() {
		if r := recover(); r != nil {
			switch sig := r.(type) {
			case returnSignal:
				result = sig.value
			case signalError:
				panic(sig)
			default:
				panic(r)
			}
		}
	}()
	for _, expr := range body {
		result = ctx.eval(expr)
	}
	return
}

// runBodyProtected is like runBody but lets signalError propagate to the caller.
// Used by onError:do: and ensure: so they can intercept the signal themselves.
func (obj *ObjectServer) runBodyProtected(ctx *EvalCtx, body []*Node) (result Value) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(returnSignal); ok {
				result = sig.value
			} else {
				panic(r) // signalError or real panic — let it through
			}
		}
	}()
	for _, expr := range body {
		result = ctx.eval(expr)
	}
	return
}

// callBlockMethod handles messages sent to a block object.
// Blocks respond to: value, value:, value:value:, value:value:value:, free
func (obj *ObjectServer) callBlockMethod(name string, args []Value) Value {
	bd := obj.blockDef
	switch {
	case name == "free":
		obj.shutdownAndExit()
		return Nil

	case name == "value" || strings.HasPrefix(name, "value:"):
		ctx := obj.blockCtx(bd, args)
		return obj.runBody(ctx, bd.Body)

	case name == "whileTrue:":
		// [condBlock] whileTrue: bodyBlock
		if len(args) < 1 {
			return Nil
		}
		bodyRef := args[0]
		for obj.callBlockMethod("value", nil).Truthy() {
			obj.sendMessage(bodyRef, "value", nil)
		}
		return Nil

	case name == "whileFalse:":
		// [condBlock] whileFalse: bodyBlock
		if len(args) < 1 {
			return Nil
		}
		bodyRef := args[0]
		for !obj.callBlockMethod("value", nil).Truthy() {
			obj.sendMessage(bodyRef, "value", nil)
		}
		return Nil

	case name == "repeat":
		// [body] repeat — run body forever; escape via signal / onError:.
		ctx := obj.blockCtx(bd, nil)
		for {
			obj.runBody(ctx, bd.Body)
		}

	case name == "onError:" || name == "onError:do:" || name == "on:do:":
		// [risky] onError: handlerBlock
		// [risky] on: anything do: handlerBlock     (the `on:` value is an
		//   ignored placeholder — this dialect has no exception classes)
		// Evaluates own body; if a signal fires, sends value: errorMsg to
		// handlerBlock.  Returns own body's result on success, or the
		// handler's result on error.  The handler is always the LAST arg,
		// so all three spellings (and the combined-selector S-expr form)
		// land the handler correctly.
		if len(args) < 1 {
			return Nil
		}
		handlerRef := args[len(args)-1]
		ctx := obj.blockCtx(bd, nil)
		var caught *signalError
		result := func() (v Value) {
			defer func() {
				if r := recover(); r != nil {
					if sig, ok := r.(signalError); ok {
						caught = &sig
					} else {
						panic(r)
					}
				}
			}()
			return obj.runBodyProtected(ctx, bd.Body)
		}()
		if caught != nil {
			return obj.sendMessage(handlerRef, "value:", []Value{StrVal(caught.msg)})
		}
		return result

	case name == "ensure:":
		// [body] ensure: cleanupBlock
		// Always sends value to cleanupBlock after own body, even if a signal fired.
		if len(args) < 1 {
			return Nil
		}
		cleanupRef := args[0]
		ctx := obj.blockCtx(bd, nil)
		var panicked interface{}
		result := func() (v Value) {
			defer func() {
				if r := recover(); r != nil {
					panicked = r
				}
			}()
			return obj.runBodyProtected(ctx, bd.Body)
		}()
		obj.sendMessage(cleanupRef, "value", nil) // always runs
		if panicked != nil {
			// Re-raise after cleanup. When ensure: was reached via an RPC
			// send (the usual case — the receiver block is its own process),
			// this panic is recovered by dispatchToReply and encoded into the
			// reply, so sendMessage re-raises it in the caller where an
			// enclosing onError: can catch it. Without that boundary handling
			// this would crash the handler goroutine and hang the caller.
			panic(panicked)
		}
		return result
	}

	// Last shot before doesNotUnderstand: did the caller use the block's
	// own param names as the keyword selector?  e.g. [c | c + 1] c: 41,
	// or [x y | x + y] x: 3 y: 4.  We accept any ordering of the
	// keywords and reorder the args to match `bd.Params`.  This lets a
	// caller name the slots without forcing them to remember
	// `value:value:`-style positional calls — but `value`/`value:` etc.
	// still work, so a block passed to `arr do:` or any other consumer
	// expecting positional invocation continues to fit.
	if reordered, ok := blockKeywordsMatchParams(name, bd.Params, args); ok {
		ctx := obj.blockCtx(bd, reordered)
		return obj.runBody(ctx, bd.Body)
	}

	return unsupportedMessage(ObjVal(obj.id), "Block", name, args)
}

// blockKeywordsMatchParams returns the call args reordered to match
// `params` (in declaration order) when the keyword parts of `name`
// equal the param-name set, regardless of the order they appeared
// at the call site.  Returns (nil, false) when the selector isn't
// a keyword form, has the wrong arity, contains a duplicate
// keyword, or names a slot the block doesn't have.
func blockKeywordsMatchParams(name string, params []string, args []Value) ([]Value, bool) {
	if !strings.HasSuffix(name, ":") || len(params) == 0 {
		return nil, false
	}
	keywords := strings.Split(strings.TrimSuffix(name, ":"), ":")
	if len(keywords) != len(params) || len(args) != len(params) {
		return nil, false
	}
	pos := make(map[string]int, len(keywords))
	for i, k := range keywords {
		if _, dup := pos[k]; dup {
			return nil, false
		}
		pos[k] = i
	}
	out := make([]Value, len(params))
	for i, p := range params {
		idx, ok := pos[p]
		if !ok {
			return nil, false
		}
		out[i] = args[idx]
	}
	return out, true
}

// blockCtx builds an EvalCtx for evaluating a block's body.
// callArgs are the values passed at the call site, bound to bd.Params in order.
func (obj *ObjectServer) blockCtx(bd *BlockDef, callArgs []Value) *EvalCtx {
	ctx := &EvalCtx{
		obj:      obj,
		args:     make(map[string]Value),
		isBlock:  true,
		parentID: bd.ParentID,
		selfOverride: bd.SelfOverride,
	}
	for k, v := range bd.CapturedArgs {
		ctx.args[k] = v
	}
	for i, p := range bd.Params {
		if i < len(callArgs) {
			ctx.args[p] = callArgs[i]
		}
	}
	return ctx
}

// articleFor returns the Smalltalk-flavoured "a Counter" / "an Array"
// printString default for a class name.  Internal class names
// (`__array__` etc.) are remapped to the user-facing form first.
func articleFor(className string) string {
	user := className
	switch className {
	case BuiltinClassArray:
		user = UserClassArray
	case BuiltinClassDictionary:
		user = UserClassDictionary
	case BuiltinClassHttpClient:
		user = UserClassHttpClient
	case BuiltinClassHttpServer:
		user = UserClassHttpServer
	case BuiltinClassRobotsMatcher:
		user = UserClassRobotsMatcher
	case BuiltinClassSitemapParser:
		user = UserClassSitemapParser
	case BuiltinClassProcess:
		user = UserClassProcess
	case BuiltinClassBlock:
		user = "Block"
	case BuiltinClassWorkspace:
		user = "Workspace"
	case BuiltinClassCoordinator:
		user = "Coordinator"
	}
	if user == "" {
		return "an Object"
	}
	first := []rune(user)[0]
	switch first {
	case 'A', 'E', 'I', 'O', 'U':
		return "an " + user
	}
	return "a " + user
}

// formatValueForPrint renders a Value the way Dictionary printString
// shows its values — quoted strings, recursive printString for objids,
// otherwise Value.String().
func (obj *ObjectServer) formatValueForPrint(v Value) string {
	switch v.Kind {
	case "string":
		return "'" + strings.ReplaceAll(v.Str, "'", "''") + "'"
	case "objid":
		ps := obj.sendMessage(v, "printString", nil)
		if ps.Kind == "string" {
			return ps.Str
		}
		return v.String()
	}
	return v.String()
}

// formatValueInArray renders an entry inside Array's printString —
// strings are shown raw (no surrounding quotes) so `ls` prints file
// names cleanly; objids recurse through their own printString; other
// kinds fall back to Value.String().
func (obj *ObjectServer) formatValueInArray(v Value) string {
	switch v.Kind {
	case "string":
		return v.Str
	case "objid":
		ps := obj.sendMessage(v, "printString", nil)
		if ps.Kind == "string" {
			return ps.Str
		}
		return v.String()
	}
	return v.String()
}

// callArrayMethod handles messages sent to an array object.
func (obj *ObjectServer) callArrayMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		obj.mu.Lock()
		obj.arrayData = make([]Value, 0)
		obj.mu.Unlock()
		return Nil

	case "size":
		obj.mu.RLock()
		n := len(obj.arrayData)
		obj.mu.RUnlock()
		return NumVal(float64(n))

	case "add:":
		if len(args) == 0 {
			return Nil
		}
		obj.mu.Lock()
		obj.arrayData = append(obj.arrayData, args[0])
		obj.mu.Unlock()
		return args[0]

	case "at:":
		if len(args) == 0 || args[0].Kind != "number" {
			return Nil
		}
		i := int(args[0].Num) // 0-based indexing
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		if i < 0 || i >= len(obj.arrayData) {
			return Nil
		}
		return obj.arrayData[i]

	case "at:put:":
		if len(args) < 2 || args[0].Kind != "number" {
			return Nil
		}
		i := int(args[0].Num)
		if i < 0 {
			return Nil
		}
		obj.mu.Lock()
		defer obj.mu.Unlock()
		for len(obj.arrayData) <= i {
			obj.arrayData = append(obj.arrayData, Nil)
		}
		obj.arrayData[i] = args[1]
		return args[1]

	case "do:":
		// Send value: to the given block for each element
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		for _, elem := range snapshot {
			obj.sendMessage(block, "value:", []Value{elem})
		}
		return Nil

	case "collect:":
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		// Spawn a new array for the result
		resultID := obj.spawnBuiltin(BuiltinClassArray)
		if resultID == "" {
			return Nil
		}
		result := ObjVal(resultID)
		for _, elem := range snapshot {
			mapped := obj.sendMessage(block, "value:", []Value{elem})
			obj.sendMessage(result, "add:", []Value{mapped})
		}
		return result

	case "includes:":
		if len(args) == 0 {
			return Nil
		}
		target := args[0].String()
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		for _, v := range obj.arrayData {
			if v.String() == target {
				return True
			}
		}
		return False

	case "first":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		if len(obj.arrayData) == 0 {
			return Nil
		}
		return obj.arrayData[0]

	case "last":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		if len(obj.arrayData) == 0 {
			return Nil
		}
		return obj.arrayData[len(obj.arrayData)-1]

	case "printAll":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		for i, v := range obj.arrayData {
			obj.transcriptf("  [%d] %s", i, v.String())
		}
		return Nil

	case "printString":
		// One entry per line — what a shell user expects from `ls`,
		// and a generally legible form at the REPL.  Strings are
		// shown raw (no quotes); objids recurse through their own
		// printString; everything else via Value.String().  For
		// nested Arrays this nests newlines too, which can be ugly,
		// but explicit collection display IS what you asked for.
		obj.mu.RLock()
		copyData := make([]Value, len(obj.arrayData))
		copy(copyData, obj.arrayData)
		obj.mu.RUnlock()
		parts := make([]string, 0, len(copyData))
		for _, v := range copyData {
			parts = append(parts, obj.formatValueInArray(v))
		}
		return StrVal(strings.Join(parts, "\n"))

	case "isEmpty":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		return BoolVal(len(obj.arrayData) == 0)

	case "notEmpty":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		return BoolVal(len(obj.arrayData) > 0)

	case "indexOf:":
		// 0-based index of the first element equal (by printed form) to
		// the argument, or -1 — mirrors String indexOf: and includes:.
		if len(args) == 0 {
			return NumVal(-1)
		}
		target := args[0].String()
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		for i, v := range obj.arrayData {
			if v.String() == target {
				return NumVal(float64(i))
			}
		}
		return NumVal(-1)

	case "remove:":
		// Remove the first element matching the argument (by printed form);
		// returns the removed element, or nil if not present.  Mutates in place.
		if len(args) == 0 {
			return Nil
		}
		target := args[0].String()
		obj.mu.Lock()
		defer obj.mu.Unlock()
		for i, v := range obj.arrayData {
			if v.String() == target {
				obj.arrayData = append(obj.arrayData[:i], obj.arrayData[i+1:]...)
				return v
			}
		}
		return Nil

	case "removeAt:":
		// 0-based; removes and returns the element, or nil if out of range.
		if len(args) == 0 || args[0].Kind != "number" {
			return Nil
		}
		i := int(args[0].Num)
		obj.mu.Lock()
		defer obj.mu.Unlock()
		if i < 0 || i >= len(obj.arrayData) {
			return Nil
		}
		v := obj.arrayData[i]
		obj.arrayData = append(obj.arrayData[:i], obj.arrayData[i+1:]...)
		return v

	case "sort":
		// In-place ascending sort (numbers numerically, otherwise by printed
		// form); returns self.  OrderedCollection>>sort semantics.
		obj.mu.Lock()
		sort.SliceStable(obj.arrayData, func(i, j int) bool {
			return lessValue(obj.arrayData[i], obj.arrayData[j])
		})
		obj.mu.Unlock()
		return ObjVal(obj.id)

	case "sort:":
		// In-place sort with a 2-arg comparator block returning truthy when
		// the first argument should sort before the second; returns self.
		if len(args) == 0 {
			return Nil
		}
		return obj.sortArrayWith(args[0])

	case "reverse", "reversed":
		// Non-mutating: returns a new Array with elements in reverse order.
		obj.mu.RLock()
		n := len(obj.arrayData)
		rev := make([]Value, n)
		for i, v := range obj.arrayData {
			rev[n-1-i] = v
		}
		obj.mu.RUnlock()
		return obj.newArrayFrom(rev)

	case "copy", "asArray":
		// Shallow copy into a fresh Array.
		obj.mu.RLock()
		cp := make([]Value, len(obj.arrayData))
		copy(cp, obj.arrayData)
		obj.mu.RUnlock()
		return obj.newArrayFrom(cp)

	case "addAll:":
		// Append every element of the argument Array; returns the argument.
		if len(args) == 0 || args[0].Kind != "objid" {
			return Nil
		}
		elems := obj.readArrayElements(args[0])
		obj.mu.Lock()
		obj.arrayData = append(obj.arrayData, elems...)
		obj.mu.Unlock()
		return args[0]

	case "select:", "reject:":
		// New Array of the elements for which the block is (select:) or is
		// not (reject:) truthy.
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		keepWhenTrue := name == "select:"
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		var kept []Value
		for _, elem := range snapshot {
			if obj.sendMessage(block, "value:", []Value{elem}).Truthy() == keepWhenTrue {
				kept = append(kept, elem)
			}
		}
		return obj.newArrayFrom(kept)

	case "detect:", "detect:ifNone:":
		// First element for which the block is truthy.  detect: returns nil
		// when none match; detect:ifNone: evaluates the second block instead.
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		for _, elem := range snapshot {
			if obj.sendMessage(block, "value:", []Value{elem}).Truthy() {
				return elem
			}
		}
		if name == "detect:ifNone:" && len(args) >= 2 {
			return obj.sendMessage(args[1], "value", nil)
		}
		return Nil

	case "inject:into:":
		// Fold: seed = args[0], 2-arg block = args[1] called as
		// (accumulator, element) for each element; returns the final value.
		if len(args) < 2 {
			return Nil
		}
		acc := args[0]
		block := args[1]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		for _, elem := range snapshot {
			acc = obj.sendMessage(block, "value:value:", []Value{acc, elem})
		}
		return acc

	case "with:do:":
		// Parallel iteration: for each i, call the 2-arg block with
		// (self[i], other[i]).  Stops at the shorter length.
		if len(args) < 2 || args[0].Kind != "objid" {
			return Nil
		}
		other := obj.readArrayElements(args[0])
		block := args[1]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		n := len(snapshot)
		if len(other) < n {
			n = len(other)
		}
		for i := 0; i < n; i++ {
			obj.sendMessage(block, "value:value:", []Value{snapshot[i], other[i]})
		}
		return Nil

	case "parallelSend:withEach:", "parallelSend:withEach:timeoutMs:":
		minimumArguments := 2
		if name == "parallelSend:withEach:timeoutMs:" {
			minimumArguments = 3
		}
		if len(args) < minimumArguments || args[0].Kind != "string" || args[0].Str == "" || args[1].Kind != "objid" {
			return Nil
		}
		callTimeout := 30 * time.Second
		if name == "parallelSend:withEach:timeoutMs:" {
			var ok bool
			callTimeout, ok = messageTimeoutFromMilliseconds(args[2])
			if !ok {
				return Nil
			}
		}
		otherEntry, ok := obj.lookupRegistry(args[1].ObjID)
		if !ok || (otherEntry.ClassName != BuiltinClassArray && otherEntry.ClassName != UserClassArray) {
			return Nil
		}
		other := obj.readArrayElements(args[1])
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		n := len(snapshot)
		if len(other) < n {
			n = len(other)
		}
		results := make([]Value, n)
		replies := make([]ReplyRequest, n)
		var wait sync.WaitGroup
		wait.Add(n)
		for i := 0; i < n; i++ {
			go func(index int) {
				defer wait.Done()
				replies[index] = dispatchToReply("", func() Value {
					return obj.sendMessageWithTimeout(snapshot[index], args[0].Str, []Value{other[index]}, callTimeout)
				})
			}(i)
		}
		wait.Wait()
		for index, reply := range replies {
			if reply.Signaled {
				panic(signalError{msg: reply.SignalMsg})
			} else {
				results[index] = reply.Result
			}
		}
		return obj.newArrayFrom(results)

	case "max":
		// Return the largest element using the default ordering.
		// Returns Nil for an empty array.
		obj.mu.RLock()
		data := make([]Value, len(obj.arrayData))
		copy(data, obj.arrayData)
		obj.mu.RUnlock()
		if len(data) == 0 {
			return Nil
		}
		maxVal := data[0]
		for _, v := range data[1:] {
			if lessValue(maxVal, v) {
				maxVal = v
			}
		}
		return maxVal

	case "min":
		// Return the smallest element using the default ordering.
		// Returns Nil for an empty array.
		obj.mu.RLock()
		data := make([]Value, len(obj.arrayData))
		copy(data, obj.arrayData)
		obj.mu.RUnlock()
		if len(data) == 0 {
			return Nil
		}
		minVal := data[0]
		for _, v := range data[1:] {
			if lessValue(v, minVal) {
				minVal = v
			}
		}
		return minVal

	case "join:":
		// Concatenate elements as strings with the given separator.
		sep := ""
		if len(args) > 0 {
			sep = args[0].String()
		}
		obj.mu.RLock()
		strs := make([]string, len(obj.arrayData))
		for i, v := range obj.arrayData {
			strs[i] = v.String()
		}
		obj.mu.RUnlock()
		return StrVal(strings.Join(strs, sep))

	case "any:":
		// Return true if any element makes the block truthy; false otherwise.
		if len(args) < 1 {
			return False
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		for _, elem := range snapshot {
			if obj.sendMessage(block, "value:", []Value{elem}).Truthy() {
				return True
			}
		}
		return False

	case "all:":
		// Return true if every element makes the block truthy; false otherwise.
		if len(args) < 1 {
			return True
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make([]Value, len(obj.arrayData))
		copy(snapshot, obj.arrayData)
		obj.mu.RUnlock()
		for _, elem := range snapshot {
			if !obj.sendMessage(block, "value:", []Value{elem}).Truthy() {
				return False
			}
		}
		return True
	}

	return unsupportedMessage(ObjVal(obj.id), "Array", name, args)
}

// lessValue is the default ordering used by Array sort: numbers compare
// numerically, everything else by printed form.  Mixed types fall back to
// the string form so the sort is at least total and deterministic.
func lessValue(a, b Value) bool {
	if a.Kind == "number" && b.Kind == "number" {
		return a.Num < b.Num
	}
	return a.String() < b.String()
}

// newArrayFrom spawns a fresh built-in Array seeded with elems.  Shared by
// the Array methods that return a new collection (reverse, copy, select:, …).
// Returns nil if the spawn fails (no coordinator reachable).
func (obj *ObjectServer) newArrayFrom(elems []Value) Value {
	id := obj.spawnBuiltin(BuiltinClassArray)
	if id == "" {
		return Nil
	}
	ref := ObjVal(id)
	for _, v := range elems {
		obj.sendMessage(ref, "add:", []Value{v})
	}
	return ref
}

// callDictionaryMethod handles messages sent to a Dictionary object.
func (obj *ObjectServer) callDictionaryMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		obj.mu.Lock()
		obj.dictionaryData = make(map[string]Value)
		obj.mu.Unlock()
		return Nil

	case "size":
		obj.mu.RLock()
		n := len(obj.dictionaryData)
		obj.mu.RUnlock()
		return NumVal(float64(n))

	case "at:":
		if len(args) == 0 {
			return Nil
		}
		key := args[0].String()
		obj.mu.RLock()
		v := obj.dictionaryData[key]
		obj.mu.RUnlock()
		return v

	case "at:put:":
		if len(args) < 2 {
			return Nil
		}
		key := args[0].String()
		obj.mu.Lock()
		obj.dictionaryData[key] = args[1]
		obj.mu.Unlock()
		return args[1]

	case "at:ifAbsent:":
		// (send dict at: key ifAbsent: block) — calls block value if key missing
		if len(args) < 2 {
			return Nil
		}
		key := args[0].String()
		obj.mu.RLock()
		v, ok := obj.dictionaryData[key]
		obj.mu.RUnlock()
		if ok {
			return v
		}
		return obj.sendMessage(args[1], "value", nil)

	case "includesKey:":
		if len(args) == 0 {
			return Nil
		}
		key := args[0].String()
		obj.mu.RLock()
		_, ok := obj.dictionaryData[key]
		obj.mu.RUnlock()
		return BoolVal(ok)

	case "removeKey:":
		if len(args) == 0 {
			return Nil
		}
		key := args[0].String()
		obj.mu.Lock()
		old := obj.dictionaryData[key]
		delete(obj.dictionaryData, key)
		obj.mu.Unlock()
		return old

	case "do:":
		// Send value: to block for each value
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		vals := make([]Value, 0, len(obj.dictionaryData))
		for _, v := range obj.dictionaryData {
			vals = append(vals, v)
		}
		obj.mu.RUnlock()
		for _, v := range vals {
			obj.sendMessage(block, "value:", []Value{v})
		}
		return Nil

	case "keysAndValuesDo:":
		// Send value:value: to block for each key-value pair
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		pairs := make([][2]Value, 0, len(obj.dictionaryData))
		for k, v := range obj.dictionaryData {
			pairs = append(pairs, [2]Value{StrVal(k), v})
		}
		obj.mu.RUnlock()
		for _, p := range pairs {
			obj.sendMessage(block, "value:value:", []Value{p[0], p[1]})
		}
		return Nil

	case "keys":
		// Returns a new Array containing all keys as strings
		id := obj.spawnBuiltin(BuiltinClassArray)
		if id == "" {
			return Nil
		}
		arr := ObjVal(id)
		obj.mu.RLock()
		keys := make([]string, 0, len(obj.dictionaryData))
		for k := range obj.dictionaryData {
			keys = append(keys, k)
		}
		obj.mu.RUnlock()
		for _, k := range keys {
			obj.sendMessage(arr, "add:", []Value{StrVal(k)})
		}
		return arr

	case "values":
		// Returns a new Array containing all values
		id := obj.spawnBuiltin(BuiltinClassArray)
		if id == "" {
			return Nil
		}
		arr := ObjVal(id)
		obj.mu.RLock()
		vals := make([]Value, 0, len(obj.dictionaryData))
		for _, v := range obj.dictionaryData {
			vals = append(vals, v)
		}
		obj.mu.RUnlock()
		for _, v := range vals {
			obj.sendMessage(arr, "add:", []Value{v})
		}
		return arr

	case "printAll":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		for k, v := range obj.dictionaryData {
			obj.transcriptf("  %q -> %s", k, v.String())
		}
		return Nil

	case "printString":
		// One `key -> value` per line, keys sorted for stable output.
		// Same Smalltalk arrow style as `at:put:`; values render via
		// formatValueForPrint (recursive printString for objids,
		// quoted strings, otherwise Value.String()).
		obj.mu.RLock()
		keys := make([]string, 0, len(obj.dictionaryData))
		for k := range obj.dictionaryData {
			keys = append(keys, k)
		}
		copyData := make(map[string]Value, len(obj.dictionaryData))
		for k, v := range obj.dictionaryData {
			copyData[k] = v
		}
		obj.mu.RUnlock()
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, "'"+strings.ReplaceAll(k, "'", "''")+"' -> "+obj.formatValueForPrint(copyData[k]))
		}
		return StrVal(strings.Join(parts, "\n"))

	case "isEmpty":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		return BoolVal(len(obj.dictionaryData) == 0)

	case "notEmpty":
		obj.mu.RLock()
		defer obj.mu.RUnlock()
		return BoolVal(len(obj.dictionaryData) > 0)

	case "at:ifAbsentPut:":
		// Returns the value at key; if absent, evaluates the block, stores
		// the result under key, and returns it.
		if len(args) < 2 {
			return Nil
		}
		key := args[0].String()
		obj.mu.RLock()
		v, ok := obj.dictionaryData[key]
		obj.mu.RUnlock()
		if ok {
			return v
		}
		computed := obj.sendMessage(args[1], "value", nil)
		obj.mu.Lock()
		obj.dictionaryData[key] = computed
		obj.mu.Unlock()
		return computed

	case "copy":
		// Shallow copy into a fresh Dictionary.
		obj.mu.RLock()
		cp := make(map[string]Value, len(obj.dictionaryData))
		for k, v := range obj.dictionaryData {
			cp[k] = v
		}
		obj.mu.RUnlock()
		return obj.newDictionaryFrom(cp)

	case "collect:":
		// New Dictionary with the same keys and each value mapped through
		// the 1-arg block.
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		obj.mu.RLock()
		snapshot := make(map[string]Value, len(obj.dictionaryData))
		for k, v := range obj.dictionaryData {
			snapshot[k] = v
		}
		obj.mu.RUnlock()
		out := make(map[string]Value, len(snapshot))
		for k, v := range snapshot {
			out[k] = obj.sendMessage(block, "value:", []Value{v})
		}
		return obj.newDictionaryFrom(out)

	case "select:", "reject:":
		// New Dictionary of the entries whose value makes the block truthy
		// (select:) or falsy (reject:).  Keys are preserved.
		if len(args) == 0 {
			return Nil
		}
		block := args[0]
		keepWhenTrue := name == "select:"
		obj.mu.RLock()
		snapshot := make(map[string]Value, len(obj.dictionaryData))
		for k, v := range obj.dictionaryData {
			snapshot[k] = v
		}
		obj.mu.RUnlock()
		out := make(map[string]Value)
		for k, v := range snapshot {
			if obj.sendMessage(block, "value:", []Value{v}).Truthy() == keepWhenTrue {
				out[k] = v
			}
		}
		return obj.newDictionaryFrom(out)

	case "at:ifPresent:":
		// If the key exists, call the block with the value and return the
		// block's result.  If absent, return Nil without calling the block.
		if len(args) < 2 {
			return Nil
		}
		key := args[0].String()
		block := args[1]
		obj.mu.RLock()
		v, ok := obj.dictionaryData[key]
		obj.mu.RUnlock()
		if !ok {
			return Nil
		}
		return obj.sendMessage(block, "value:", []Value{v})
	}

	return unsupportedMessage(ObjVal(obj.id), "Dictionary", name, args)
}

// newDictionaryFrom spawns a fresh built-in Dictionary seeded with entries.
// Shared by the Dictionary methods that return a new collection (copy,
// collect:, select:).  Returns nil if the spawn fails.
func (obj *ObjectServer) newDictionaryFrom(entries map[string]Value) Value {
	id := obj.spawnBuiltin(BuiltinClassDictionary)
	if id == "" {
		return Nil
	}
	ref := ObjVal(id)
	for k, v := range entries {
		obj.sendMessage(ref, "at:put:", []Value{StrVal(k), v})
	}
	return ref
}

// spawnBuiltin asks the coordinator to spawn a built-in class and pre-populates
// the local registry cache.  Returns the new object's ID or "" on failure.
func (obj *ObjectServer) spawnBuiltin(className string) string {
	body, _ := json.Marshal(SpawnRequest{ClassName: className})
	resp, err := rpcClient.Post(obj.coordinatorURL+"/spawn", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var sr SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		obj.transcriptf("spawnBuiltin %s: decode error: %v", className, err)
		return ""
	}
	if sr.Error != "" || sr.ObjectID == "" {
		return ""
	}
	obj.registry.Store(sr.ObjectID, ObjectEntry{
		ID: sr.ObjectID, Subject: sr.Subject, ClassName: className,
	})
	return sr.ObjectID
}

// ---- Message sending ----

// sendMessage sends a Smalltalk message to target and blocks until the reply
// arrives.  Self-sends bypass HTTP.
func (obj *ObjectServer) sendMessage(target Value, method string, args []Value) Value {
	if target.Kind != "objid" {
		fmt.Fprintf(os.Stderr, "[%s] sendMessage: target is not an object ID: %v\n", obj.id, target)
		return Nil
	}

	entry, ok := obj.lookupRegistry(target.ObjID)
	if !ok {
		fmt.Fprintf(os.Stderr, "[%s] sendMessage: unknown object %s\n", obj.id, target.ObjID)
		return Nil
	}

	return obj.sendMessageNATS(entry, method, args)
}

func (obj *ObjectServer) sendMessageWithTimeout(target Value, method string, args []Value, timeout time.Duration) Value {
	if target.Kind != "objid" || timeout <= 0 {
		return Nil
	}
	entry, ok := obj.lookupRegistry(target.ObjID)
	if !ok {
		return Nil
	}
	return obj.sendMessageNATSWithTimeout(entry, method, args, timeout)
}

// performArgs recognises the perform: family on primitive receivers
// (number/string/bool/nil) and unpacks it into the real selector and
// argument slice, so each callXMethod dispatcher can re-enter itself.
// Returns ok=false for any non-perform selector.  perform:withArguments:
// reads its args out of the supplied Array.
func performArgs(obj *ObjectServer, method string, args []Value) (string, []Value, bool) {
	switch method {
	case "perform:", "perform:with:", "perform:with:with:", "perform:with:with:with:":
		if len(args) < 1 {
			return "", nil, false
		}
		return args[0].String(), args[1:], true
	case "perform:withArguments:":
		if len(args) < 1 {
			return "", nil, false
		}
		var callArgs []Value
		if len(args) >= 2 && args[1].Kind == "objid" && obj != nil {
			callArgs = obj.readArrayElements(args[1])
		}
		return args[0].String(), callArgs, true
	}
	return "", nil, false
}

// readArrayElements reads the elements of an Array object into a Go
// slice via its size / at: methods.  Used by perform:withArguments:.
// Non-Array or unreachable targets yield an empty slice.
func (obj *ObjectServer) readArrayElements(arr Value) []Value {
	sz := obj.sendMessage(arr, "size", nil)
	if sz.Kind != "number" {
		return nil
	}
	n := int(sz.Num)
	out := make([]Value, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, obj.sendMessage(arr, "at:", []Value{NumVal(float64(i))}))
	}
	return out
}

// ---- Transcript ----

func (obj *ObjectServer) transcriptf(format string, a ...any) {
	text := fmt.Sprintf(format, a...)

	// If an eval: call is in progress on this object, capture locally.
	obj.evalCaptureMu.Lock()
	if obj.evalCapturing {
		obj.evalCapture = append(obj.evalCapture, text)
	}
	obj.evalCaptureMu.Unlock()

	if obj.blockDef != nil && obj.blockDef.CaptureObjectID != "" {
		obj.sendMessage(ObjVal(obj.blockDef.CaptureObjectID), "__capture:", []Value{StrVal(text)})
	}

	req := TranscriptRequest{ObjectID: obj.id, Text: text}
	body, _ := json.Marshal(req)
	resp, err := rpcClient.Post(obj.coordinatorURL+"/transcript", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Printf("[%s] transcript error: %v\n", obj.id, err)
		return
	}
	resp.Body.Close()
}

// handleCapture receives print output forwarded from child blocks and appends
// it to the workspace eval capture buffer (only when evalCapturing == true).
func (obj *ObjectServer) handleCapture(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	obj.evalCaptureMu.Lock()
	if obj.evalCapturing {
		obj.evalCapture = append(obj.evalCapture, req.Text)
	}
	obj.evalCaptureMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// ---- Legacy synchronous /call endpoint ----

// handleCall executes a method synchronously and returns the result directly.
// Normal eval traffic uses NATS.
func (obj *ObjectServer) handleCall(w http.ResponseWriter, r *http.Request) {
	var req CallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	result := obj.callMethod(req.Method, req.Args)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CallResponse{Result: result})
}

// ---- Legacy introspection /inspect endpoint ----

// handleInspect returns class, methods, and current variable state for this object.
func (obj *ObjectServer) handleInspect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(obj.inspectResponse())
}

func (obj *ObjectServer) inspectResponse() InspectResponse {
	vars := make(map[string]Value)

	switch obj.classDef.Name {
	case BuiltinClassArray:
		obj.mu.RLock()
		for i, v := range obj.arrayData {
			vars[fmt.Sprintf("[%d]", i)] = v
		}
		obj.mu.RUnlock()
	case BuiltinClassDictionary:
		obj.mu.RLock()
		for k, v := range obj.dictionaryData {
			vars[k] = v
		}
		obj.mu.RUnlock()
	case BuiltinClassBlock:
		if obj.blockDef != nil {
			vars["(parent)"] = ObjVal(obj.blockDef.ParentID)
			for k, v := range obj.blockDef.CapturedArgs {
				vars["(captured) "+k] = v
			}
		}
	default:
		obj.mu.RLock()
		for k, v := range obj.instanceVars {
			vars[k] = v
		}
		obj.mu.RUnlock()
	}

	methods := obj.inspectionMethodNames()

	return InspectResponse{
		ObjectID:     obj.id,
		ClassName:    obj.classDef.Name,
		Methods:      methods,
		MethodSignatures: inspectorSignatures(obj.classDef),
		InstanceVars: vars,
		Runtime:      obj.runtimeInspection(),
	}
}

func (obj *ObjectServer) metricsResponse() ObjectMetrics {
	metrics := ObjectMetrics{
		ObjectID:        obj.id,
		ClassName:       "",
		Subject:         natsSubjectForObject(obj.id),
		CallsReceived:   obj.callsReceived.Load(),
		CallsCompleted:  obj.callsCompleted.Load(),
		MailboxRejected: obj.mailboxRejected.Load(),
	}
	if obj.classDef != nil {
		metrics.ClassName = obj.classDef.Name
	}
	if obj.mailbox != nil {
		metrics.MailboxDepth = len(obj.mailbox)
		metrics.MailboxCapacity = cap(obj.mailbox)
	}
	if obj.controlMailbox != nil {
		metrics.ControlMailboxDepth = len(obj.controlMailbox)
		metrics.ControlMailboxCapacity = cap(obj.controlMailbox)
	}
	return metrics
}

// ---- Workspace eval: ----

// callWorkspaceMethod handles messages to the __workspace__ object.
// The eval: method parses and evaluates arbitrary code, capturing print output.
// State accumulates in instanceVars between calls (REPL semantics).
func (obj *ObjectServer) callWorkspaceMethod(method string, args []Value) Value {
	switch method {
	case "initialize":
		return ObjVal(obj.id)
	case "eval:", "evalScript:", "evalResult:", "evalInternal:":
		if len(args) < 1 || args[0].Kind != "string" {
			return StrVal("error: eval: requires a code string")
		}
		nodes, err := parseWorkspaceSource(args[0].Str)
		if err != nil {
			if method == "evalScript:" || method == "evalResult:" || method == "evalInternal:" {
				panic(signalError{msg: fmt.Sprintf("parse error: %v", err)})
			} else {
				return StrVal(fmt.Sprintf("parse error: %v", err))
			}
		}

		// Serialise concurrent evals
		obj.evalSerial.Lock()
		defer obj.evalSerial.Unlock()

		// Start capturing transcript output
		obj.evalCaptureMu.Lock()
		obj.evalCapturing = true
		obj.evalCapture = nil
		obj.evalCaptureMu.Unlock()

		defer func() {
			obj.evalCaptureMu.Lock()
			obj.evalCapturing = false
			obj.evalCaptureMu.Unlock()
		}()

		ctx := &EvalCtx{obj: obj, args: make(map[string]Value)}
		// `smt shell` sets workspace.shellContext to a Shell objid;
		// while it's set, every eval runs with `self` overridden to
		// that Shell, so `ls`, `cd: '/tmp'`, etc. dispatch as method
		// calls on the Shell rather than the workspace.
		obj.mu.RLock()
		sc, hasSC := obj.instanceVars["shellContext"]
		obj.mu.RUnlock()
		if hasSC && sc.Kind == "objid" && method != "evalInternal:" {
			scCopy := sc
			ctx.selfOverride = &scCopy
		}
		var result Value
		if method == "evalScript:" || method == "evalResult:" || method == "evalInternal:" {
			result = obj.runBodyProtected(ctx, nodes)
		} else {
			result = obj.runBody(ctx, nodes)
		}

		obj.evalCaptureMu.Lock()
		lines := make([]string, len(obj.evalCapture))
		copy(lines, obj.evalCapture)
		obj.evalCaptureMu.Unlock()

		if method == "evalInternal:" {
			return result
		} else {
			// Infrastructure evaluation preserves the user's last result.
		}
		if method == "evalResult:" {
			obj.mu.Lock()
			obj.instanceVars["it"] = result
			obj.mu.Unlock()
			data, err := json.Marshal(WorkspaceResult{Result: result, Output: strings.Join(lines, "\n")})
			if err != nil { panic(signalError{msg: fmt.Sprintf("encode workspace result: %v", err)}) } else { return StrVal(string(data)) }
		} else {
			// Existing workspace methods retain their textual result format.
		}

		// Build output: all printed lines, then "=> result" if non-nil
		// and there were no print lines (avoid echoing print return values).
		var sb strings.Builder
		for _, line := range lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		showResult := len(lines) == 0 || (result.Kind != "nil" && result.Kind != "string")
		if showResult {
			if sb.Len() > 0 {
				sb.WriteString("=> ")
			}
			// For objid results, ask the object for its printString
			// (built-ins have content-aware overrides; the universal
			// fallback returns "a Counter").  Other Value kinds
			// render via the existing String() form.
			if result.Kind == "objid" {
				ps := obj.sendMessage(result, "printString", nil)
				if ps.Kind == "string" {
					sb.WriteString(ps.Str)
				} else {
					sb.WriteString(result.String())
				}
			} else {
				sb.WriteString(result.String())
			}
		}
		out := strings.TrimRight(sb.String(), "\n")
		if out == "" && result.Kind != "string" {
			out = "nil"
		}
		return StrVal(out)

	default:
		return unsupportedMessage(ObjVal(obj.id), "Workspace", method, args)
	}
}
