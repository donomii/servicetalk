package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

// Value is everything that crosses the wire.
// Only numbers, strings, object IDs, booleans, and nil are allowed.
type Value struct {
	Kind  string  `json:"kind"` // "number" | "string" | "objid" | "bool" | "character" | "nil"
	Num   float64 `json:"num,omitempty"`
	Str   string  `json:"str,omitempty"`
	ObjID string  `json:"objid,omitempty"`
	Bool  bool    `json:"bool,omitempty"`
	Char  rune    `json:"char,omitempty"` // Unicode code point; only for kind "character"
}

var (
	Nil   = Value{Kind: "nil"}
	True  = Value{Kind: "bool", Bool: true}
	False = Value{Kind: "bool", Bool: false}
)

func NumVal(n float64) Value { return Value{Kind: "number", Num: n} }
func StrVal(s string) Value  { return Value{Kind: "string", Str: s} }
func ObjVal(id string) Value { return Value{Kind: "objid", ObjID: id} }
func CharVal(r rune) Value   { return Value{Kind: "character", Char: r} }
func BoolVal(b bool) Value {
	if b {
		return True
	}
	return False
}

func (v Value) String() string {
	switch v.Kind {
	case "number":
		if v.Num == float64(int64(v.Num)) {
			return fmt.Sprintf("%d", int64(v.Num))
		}
		return fmt.Sprintf("%g", v.Num)
	case "string":
		return v.Str
	case "objid":
		// Print as `#@id` so the Smalltalk parser can round-trip
		// REPL output back as an objid literal.
		return "#@" + v.ObjID
	case "bool":
		if v.Bool {
			return "true"
		}
		return "false"
	case "character":
		return string(v.Char)
	default:
		return "nil"
	}
}

func (v Value) Truthy() bool {
	switch v.Kind {
	case "nil":
		return false
	case "bool":
		return v.Bool
	case "number":
		return v.Num != 0
	default:
		return true
	}
}

// ObjectEntry is a registry record for one running object.
type ObjectEntry struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject,omitempty"`
	ClassName string    `json:"className"`
	LastSeen  time.Time `json:"-"` // not serialised over the wire
}

// ---- Wire types ----

type MessageRequest struct {
	Sender string `json:"sender,omitempty"`
	MsgID   string  `json:"msgId"`
	ChainID string  `json:"chainId,omitempty"`
	Method  string  `json:"method"`
	Args    []Value `json:"args"`
}

type ReplyRequest struct {
	MsgID  string `json:"msgId"`
	Result Value  `json:"result"`
	// Signaled is set when the remote callMethod terminated by a signalError
	// that escaped (e.g. an ensure: re-raise, or a bare `signal` send). The
	// caller re-raises it locally in sendMessage so an enclosing onError:/
	// ensure: in the caller's process can handle it; the reply carries no
	// usable Result in that case.
	Signaled  bool   `json:"signaled,omitempty"`
	SignalMsg string `json:"signalMsg,omitempty"`
}

type RegisterRequest struct {
	ObjectID  string `json:"objectId"`
	ClassName string `json:"className"`
	Subject   string `json:"subject,omitempty"`
}

type SpawnRequest struct {
	Snapshot         *NativeSnapshot `json:"snapshot,omitempty"`
	ClassName        string        `json:"className"`
	DefFile          string        `json:"defFile,omitempty"`          // block def file path (local spawn only)
	BlockDef         *BlockDef     `json:"blockDef,omitempty"`         // inline block definition (for remote spawn)
	PulseTimeoutSecs int           `json:"pulseTimeoutSecs,omitempty"` // pulse-GC timeout override
	AutoInit         bool          `json:"autoInit,omitempty"`         // call initialize on startup
	SourceBundle     *SourceBundle `json:"sourceBundle,omitempty"`     // class source cache for remote workers
}

type SourceBundle struct {
	Digest string             `json:"digest"`
	Files  []SourceBundleFile `json:"files"`
}

type SourceBundleFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

// PulseRequest is the payload behind pulse: — the sole input to the pulse
// reachability tracer.  SweepID is time.Now().UnixMilli() at the root.
// An object updates its lastSweepID and re-pulses its references only
// when the incoming SweepID is strictly greater than what it last saw,
// which is what breaks cycles.
type PulseRequest struct {
	SweepID int64 `json:"sweepId"`
}

type DeregisterRequest struct {
	ObjectID string `json:"objectId"`
}

// BlockDef is the serialised definition of a block closure.
// Written to a temp file by the spawning object; read on block startup.
type BlockDef struct {
	SelfOverride *Value `json:"selfOverride,omitempty"`
	Params          []string         `json:"params"`
	Body            []*Node          `json:"body"`
	ParentID        string           `json:"parentId"`
	CapturedArgs    map[string]Value `json:"capturedArgs"`
	CaptureObjectID string           `json:"captureObjectId,omitempty"`
}

type SpawnResponse struct {
	ObjectID string `json:"objectId,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WorkerEntry is a registry record for a connected worker node.
type WorkerEntry struct {
	ID             string    `json:"id"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	InFlightSpawns int       `json:"inFlightSpawns"`
	AssignedSpawns uint64    `json:"assignedSpawns"`
	LastSeen       time.Time `json:"-"`
}

func (w WorkerEntry) URL() string {
	return fmt.Sprintf("http://%s:%d", w.Host, w.Port)
}

// WorkerHeartbeatRequest is a self-describing upsert: every beat carries
// the worker's host/port, so the coordinator can (re)create the registry
// entry from any heartbeat.  A coordinator restart needs no special
// handling — the next beat repopulates the worker table.  There is no
// separate registration message.
type WorkerHeartbeatRequest struct {
	WorkerID string `json:"workerId"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// RegistryResponse carries the full live-object registry.
// Returned by /registry; consumed by objects on lookup miss and by
// workers waiting for a freshly-spawned object to register.
type HeartbeatResponse struct {
	Registry []ObjectEntry `json:"registry"`
}

type MetricsResponse struct {
	RegistrySize int                `json:"registrySize"`
	RootCount    int                `json:"rootCount"`
	WorkerCount  int                `json:"workerCount"`
	Runtime      RuntimeMetrics     `json:"runtime"`
	Coordinator  CoordinatorMetrics `json:"coordinator"`
	Objects      []ObjectMetrics    `json:"objects,omitempty"`
}

type RuntimeMetrics struct {
	CallsIn      uint64 `json:"callsIn"`
	CallsOut     uint64 `json:"callsOut"`
	CallTimeouts uint64 `json:"callTimeouts"`
	QueueFull    uint64 `json:"queueFull"`
}

type CoordinatorMetrics struct {
	SpawnCount        uint64 `json:"spawnCount"`
	SpawnErrorCount   uint64 `json:"spawnErrorCount"`
	SpawnLatencyMsSum uint64 `json:"spawnLatencyMsSum"`
	SpawnLatencyMsMax uint64 `json:"spawnLatencyMsMax"`
	PulseCount        uint64 `json:"pulseCount"`
	PulseErrorCount   uint64 `json:"pulseErrorCount"`
	PulseLatencyMsSum uint64 `json:"pulseLatencyMsSum"`
	PulseLatencyMsMax uint64 `json:"pulseLatencyMsMax"`
}

type ObjectMetrics struct {
	ObjectID               string `json:"objectId"`
	ClassName              string `json:"className"`
	Subject                string `json:"subject"`
	CallsReceived          uint64 `json:"callsReceived"`
	CallsCompleted         uint64 `json:"callsCompleted"`
	MailboxRejected        uint64 `json:"mailboxRejected"`
	MailboxDepth           int    `json:"mailboxDepth"`
	MailboxCapacity        int    `json:"mailboxCapacity"`
	ControlMailboxDepth    int    `json:"controlMailboxDepth"`
	ControlMailboxCapacity int    `json:"controlMailboxCapacity"`
	Error                  string `json:"error,omitempty"`
}

type TranscriptRequest struct {
	ObjectID string `json:"objectId"`
	Text     string `json:"text"`
}

// ---- REPL / eval wire types ----

// EvalRequest is sent by the browser to coordinator /eval.
type EvalRequest struct {
	Code string `json:"code"`
}

// EvalResponse is returned by coordinator /eval.
type EvalResponse struct {
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CallRequest / CallResponse are the legacy HTTP /call shape.
type CallRequest struct {
	Method string  `json:"method"`
	Args   []Value `json:"args"`
}

type CallResponse struct {
	Result Value `json:"result"`
}

// InspectResponse is returned by inspection APIs.
type InspectResponse struct {
	ObjectID     string           `json:"objectId"`
	ClassName    string           `json:"className"`
	Methods      []string         `json:"methods"`
	MethodSignatures map[string][]string `json:"methodSignatures,omitempty"`
	InstanceVars map[string]Value `json:"instanceVars"`
	Runtime      *RuntimeInspection `json:"runtime,omitempty"`
	Retention    *RetentionInspection `json:"retention,omitempty"`
}

// CallActivity describes an executing method or an outbound wait at observation time.
type CallActivity struct {
	ID string `json:"id"`
	ChainID string `json:"chainId"`
	Method string `json:"method"`
	Target string `json:"target"`
	State string `json:"state"`
	Started time.Time `json:"started"`
}

type RuntimeInspection struct {
	Promise *PromiseSnapshot `json:"promise,omitempty"`
	Prototype string `json:"prototype,omitempty"`
	DefinitionVersion string `json:"definitionVersion,omitempty"`
	Debug *DebugSnapshot `json:"debug,omitempty"`
	Host string `json:"host"`
	PID int `json:"pid"`
	WorkerID string `json:"workerId"`
	Calls []CallActivity `json:"calls"`
	References []string `json:"references"`
	ForeignReferences bool `json:"foreignReferences"`
	PulseTimeoutMs int64 `json:"pulseTimeoutMs"`
	LastSweepID int64 `json:"lastSweepId"`
	PulseAgeMs int64 `json:"pulseAgeMs"`
	Metrics ObjectMetrics `json:"metrics"`
}

type RetentionInspection struct {
	Root bool `json:"root"`
	Path []string `json:"path"`
	Unavailable []string `json:"unavailable"`
	Note string `json:"note"`
}

// MCP envelopes retain typed JSON payloads without generic interface values.
type MCPRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID json.RawMessage `json:"id,omitempty"`
	Method string `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error *MCPError `json:"error,omitempty"`
}

type MCPError struct {
	Code int `json:"code"`
	Message string `json:"message"`
}

type MCPInitializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities json.RawMessage `json:"capabilities"`
	ClientInfo MCPClientInfo `json:"clientInfo"`
}

type MCPClientInfo struct {
	Name string `json:"name"`
	Version string `json:"version"`
}

type MCPToolCall struct {
	Name string `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type MCPToolArguments struct {
	Code string `json:"code,omitempty"`
	ObjectID string `json:"objectId,omitempty"`
	Method string `json:"method,omitempty"`
	Args []Value `json:"args,omitempty"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool `json:"isError"`
}

// ---- Utilities ----

func newUUID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// NavigationMethod indexes a definition and its syntactic message sends.
type NavigationMethod struct {
	Class string `json:"class"`
	Selector string `json:"selector"`
	Params []string `json:"params"`
	Sends []string `json:"sends"`
	Source string `json:"source"`
	File string `json:"file"`
	Line int `json:"line"`
	Foreign bool `json:"foreign"`
}

type NavigationIndex struct {
	Methods []NavigationMethod `json:"methods"`
	Errors []string `json:"errors"`
}

// WorkspaceResult preserves value identity alongside captured transcript text.
type WorkspaceResult struct {
	Result Value `json:"result"`
	Output string `json:"output"`
	Error string `json:"error,omitempty"`
}

type ObjectWorkspaceRequest struct {
	ObjectID string `json:"objectId"`
	Code string `json:"code"`
}

// Interactive UI requests share the runtime's typed Value protocol.
type InteractionRequest struct {
 TimeoutMs float64 `json:"timeoutMs,omitempty"`
 Kind string `json:"kind"`
 ObjectID string `json:"objectId"`
 Code string `json:"code"`
 Method string `json:"method"`
 Args []Value `json:"args"`
 RecordingID string `json:"recordingId"`
}
type InteractionResult struct {
 Result Value `json:"result"`
 Output string `json:"output"`
 Handle string `json:"handle,omitempty"`
 Error string `json:"error,omitempty"`
 RecordingError string `json:"recordingError,omitempty"`
}
type InteractionRecord struct {
 Request InteractionRequest `json:"request"`
 Response InteractionResult `json:"response"`
 ResultClass string `json:"resultClass,omitempty"`
 At time.Time `json:"at"`
}
type ResultRequest struct {
 Action string `json:"action"`
 Handle string `json:"handle"`
 Name string `json:"name"`
}
type RecordingRequest struct {
 Action string `json:"action"`
 ObjectID string `json:"objectId"`
}
type RecordingResponse struct {
 ObjectID string `json:"objectId,omitempty"`
 Records []InteractionRecord `json:"records"`
 Script string `json:"script"`
 Test string `json:"test"`
 Error string `json:"error,omitempty"`
}
type InspectorViewRequest struct {
 Target string `json:"target"`
 Key string `json:"key"`
 Definition json.RawMessage `json:"definition"`
}
type InspectorViewResponse struct {
 ObjectID string `json:"objectId,omitempty"`
 Definition json.RawMessage `json:"definition,omitempty"`
 Error string `json:"error,omitempty"`
}

// Live controls retain their receivers, children, arguments and Wiring objects.
type ControlProperties struct {
 Kind string `json:"kind"`
 ControlID string `json:"controlId,omitempty"`
 Label string `json:"label,omitempty"`
 Text string `json:"text,omitempty"`
 Getter string `json:"getter,omitempty"`
 Setter string `json:"setter,omitempty"`
 Method string `json:"method,omitempty"`
 Type string `json:"type,omitempty"`
 Receiver string `json:"receiver,omitempty"`
 Wiring string `json:"wiring,omitempty"`
 Refresh bool `json:"refresh,omitempty"`
 Args []Value `json:"args,omitempty"`
}
type ControlState struct {
 Properties json.RawMessage `json:"properties"`
 Children []Value `json:"children"`
}
type ControlPlan struct {
 Properties ControlProperties
 Raw map[string]json.RawMessage
 Children []*ControlPlan
 Object Value
}

// Debugger frames preserve the native evaluator context between statements.
type DebugRequest struct {
 ObjectID string `json:"objectId"`
 Method string `json:"method"`
 Args []Value `json:"args"`
}
type DebugCommand struct {
 ID string `json:"id"`
 Revision uint64 `json:"revision"`
 Action string `json:"action"`
 Code string `json:"code,omitempty"`
 Arguments map[string]Value `json:"arguments,omitempty"`
 Fields map[string]Value `json:"fields,omitempty"`
 Value Value `json:"value"`
}
type DebugSnapshot struct {
 ID string `json:"id"`
 ObjectID string `json:"objectId"`
 Method string `json:"method"`
 State string `json:"state"`
 Index int `json:"index"`
 Revision uint64 `json:"revision"`
 Arguments map[string]Value `json:"arguments"`
 Statements []string `json:"statements"`
 Result Value `json:"result"`
 Error string `json:"error,omitempty"`
 Deadline time.Time `json:"deadline"`
}
type DebugHTTPRequest struct {
 Action string `json:"action"`
 DebugRequest
 Command DebugCommand `json:"command"`
}
type DebugHTTPResponse struct {
 Snapshot DebugSnapshot `json:"snapshot"`
 Error string `json:"error,omitempty"`
}

// Trace records describe one received message and its eventual reply.
type TraceEvent struct {
 ID string `json:"id"`
 ChainID string `json:"chainId"`
 Sender string `json:"sender"`
 Receiver string `json:"receiver"`
 Method string `json:"method"`
 Args []Value `json:"args"`
 Started time.Time `json:"started"`
 Finished time.Time `json:"finished"`
 DurationMs float64 `json:"durationMs"`
 State string `json:"state"`
 Result Value `json:"result"`
 Error string `json:"error,omitempty"`
}
type TraceSnapshot struct {
 Enabled bool `json:"enabled"`
 Events []TraceEvent `json:"events"`
 Dropped uint64 `json:"dropped"`
 Errors []string `json:"errors,omitempty"`
}
type TraceRequest struct { Action string `json:"action"` }

// Source changes retain a digest for conflict detection and Git revision identity.
type SourceRequest struct {
 Action string `json:"action"`
 Class string `json:"class"`
 Source string `json:"source"`
 Digest string `json:"digest"`
 Revision string `json:"revision"`
 Message string `json:"message"`
}
type SourceRevision struct {
 ID string `json:"id"`
 Timestamp string `json:"timestamp"`
 Subject string `json:"subject"`
}
type SourceResponse struct {
 Class string `json:"class"`
 File string `json:"file"`
 Source string `json:"source"`
 Digest string `json:"digest"`
 ReadOnly bool `json:"readOnly"`
 Revisions []SourceRevision `json:"revisions"`
 HistoricalSource string `json:"historicalSource"`
 Diff string `json:"diff"`
 GitObject string `json:"gitObject"`
 GitError string `json:"gitError,omitempty"`
 Error string `json:"error,omitempty"`
}

// Native clones carry the loaded method hierarchy and shallow runtime-visible state.
type NativeDefinition struct {
 Name string `json:"name"`
 Vars []string `json:"vars"`
 Methods map[string]*MethodDef `json:"methods"`
 Superclass *NativeDefinition `json:"superclass,omitempty"`
}
type NativeSnapshot struct {
 Definition *NativeDefinition `json:"definition"`
 Fields map[string]Value `json:"fields"`
 Array []Value `json:"array,omitempty"`
 Dictionary map[string]Value `json:"dictionary,omitempty"`
 Block *BlockDef `json:"block,omitempty"`
 Prototype string `json:"prototype,omitempty"`
}
type PrototypeMethod struct {
 Name string `json:"name"`
 Params []string `json:"params"`
 Source string `json:"source"`
}
type PrototypePatch struct {
 Fields map[string]Value `json:"fields,omitempty"`
 Methods []PrototypeMethod `json:"methods,omitempty"`
}

// Message graphs connect typed values and prior message results.
type WireInput struct {
 Value *Value `json:"value,omitempty"`
 From string `json:"from,omitempty"`
}
type WireNode struct {
 ID string `json:"id"`
 Label string `json:"label,omitempty"`
 Target WireInput `json:"target"`
 Method string `json:"method"`
 Args []WireInput `json:"args"`
 X float64 `json:"x,omitempty"`
 Y float64 `json:"y,omitempty"`
}
type WireDefinition struct { Nodes []WireNode `json:"nodes"` }
type WireRun struct {
 DefinitionVersion string `json:"definitionVersion"`
 Results map[string]Value `json:"results"`
 Order []string `json:"order"`
 State string `json:"state"`
 FailedNode string `json:"failedNode,omitempty"`
 Error string `json:"error,omitempty"`
}
type WiringResponse struct {
 ObjectID string `json:"objectId"`
 Definition WireDefinition `json:"definition"`
 Run WireRun `json:"run"`
 Error string `json:"error,omitempty"`
}

// Promise state retains typed receiver, argument collection and outcome identities.
type PromiseSnapshot struct {
 State string `json:"state"`
 Target Value `json:"target"`
 Method string `json:"method"`
 Arguments Value `json:"arguments"`
 Result Value `json:"result"`
 Error string `json:"error,omitempty"`
 StartedAt string `json:"startedAt"`
 FinishedAt string `json:"finishedAt"`
 TimeoutMs float64 `json:"timeoutMs"`
}

// Live workbench tools are data owned by an ordinary object.
type WorkbenchTool struct {
 ID string `json:"id"`
 Label string `json:"label"`
 Action string `json:"action"`
 ObjectID string `json:"objectId,omitempty"`
 Method string `json:"method,omitempty"`
 Args []Value `json:"args,omitempty"`
}
type WorkbenchDefinition struct {
 Title string `json:"title"`
 Tools []WorkbenchTool `json:"tools"`
}
type WorkbenchResponse struct {
 ObjectID string `json:"objectId"`
 Definition WorkbenchDefinition `json:"definition"`
 Error string `json:"error,omitempty"`
}
type ExampleDescription struct {
 ObjectID string `json:"objectId"`
 Title string `json:"title"`
 Description string `json:"description"`
 Source string `json:"source"`
 Check string `json:"check"`
 Workspace string `json:"workspace"`
}
type ExamplesResponse struct {
 Workbench string `json:"workbench"`
 Examples []ExampleDescription `json:"examples"`
 Error string `json:"error,omitempty"`
}

// Selector locations use rune offsets so editing preserves Unicode source text.
type SelectorSpan struct { Start int; End int; Line int }
type SelectorSite struct { Selector string; Kind string; Spans []SelectorSpan }
type RefactorRequest struct {
 From string `json:"from"`
 To string `json:"to"`
 Class string `json:"class,omitempty"`
}
type RefactorEdit struct {
 Line int `json:"line"`
 Kind string `json:"kind"`
 Before string `json:"before"`
 After string `json:"after"`
}
type RefactorFile struct {
 Class string `json:"class"`
 File string `json:"file"`
 Digest string `json:"digest"`
 Source string `json:"source"`
 Edits []RefactorEdit `json:"edits"`
 Literals []int `json:"literals"`
}
type RefactorResponse struct {
 Files []RefactorFile `json:"files"`
 Errors []string `json:"errors"`
}
