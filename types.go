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
