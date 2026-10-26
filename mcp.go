package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"time"
	"unicode/utf8"
)

const mcpVersion = "2025-06-18"

// handleMCP serves stateless Streamable HTTP: JSON responses, no SSE stream.
func (c *Coordinator) handleMCP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		if parsed, err := url.Parse(origin); err != nil || parsed.Host != r.Host {
			http.Error(w, "MCP origin does not match this coordinator", http.StatusForbidden)
			return
		} else {
			// Same-origin browser clients use the same coordinator as the REPL.
		}
	} else {
		// Non-browser MCP clients normally omit Origin.
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	} else if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != mcpVersion {
		http.Error(w, "unsupported MCP protocol version: "+version, http.StatusBadRequest)
		return
	} else {
		c.readMCP(w, r)
	}
}

func (c *Coordinator) readMCP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(natsMaxCallBytes()))
	body, err := io.ReadAll(r.Body)
	var req MCPRequest
	if _, oversized := err.(*http.MaxBytesError); oversized {
		http.Error(w, "MCP request exceeds SMT_NATS_MAX_CALL_BYTES", http.StatusRequestEntityTooLarge)
	} else if err != nil || !utf8.Valid(body) || !json.Valid(body) {
		writeMCP(w, MCPResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &MCPError{-32700, "invalid MCP JSON request"}})
	} else if err := json.Unmarshal(body, &req); err != nil || !validMCPRequest(req) {
		writeMCP(w, MCPResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &MCPError{-32600, "expected a JSON-RPC 2.0 request"}})
	} else if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
	} else {
		writeMCP(w, c.dispatchMCP(req))
	}
}

func validMCPRequest(req MCPRequest) bool {
	if req.JSONRPC != "2.0" || req.Method == "" {
		return false
	} else if params := bytes.TrimSpace(req.Params); len(params) > 0 && params[0] != '{' {
		return false
	} else if len(req.ID) == 0 {
		return true
	} else {
		var text string
		var number float64
		if bytes.Equal(req.ID, []byte("null")) {
			return false
		} else if json.Unmarshal(req.ID, &text) == nil {
			return true
		} else {
			return json.Unmarshal(req.ID, &number) == nil && number == math.Trunc(number)
		}
	}
}

func writeMCP(w http.ResponseWriter, response MCPResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logInfo("MCP response write failed: %v\n", err)
	} else {
		// A single response completes the HTTP request.
	}
}

func (c *Coordinator) dispatchMCP(req MCPRequest) MCPResponse {
	response := MCPResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		response = initializeMCP(req)
	case "ping":
		response.Result = json.RawMessage(`{}`)
	case "tools/list":
		response.Result = json.RawMessage(mcpTools)
	case "tools/call":
		response = c.callMCP(req)
	default:
		response.Error = &MCPError{-32601, "unknown MCP method: " + req.Method}
	}
	return response
}

func initializeMCP(req MCPRequest) MCPResponse {
	response := MCPResponse{JSONRPC: "2.0", ID: req.ID}
	var params MCPInitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil || params.ProtocolVersion == "" || params.ClientInfo.Name == "" || params.ClientInfo.Version == "" {
		response.Error = &MCPError{-32602, "initialize requires protocolVersion, capabilities and clientInfo (name/version)"}
	} else if capabilities := bytes.TrimSpace(params.Capabilities); len(capabilities) == 0 || capabilities[0] != '{' {
		response.Error = &MCPError{-32602, "initialize capabilities must be an object"}
	} else {
		response.Result = json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"servicetalk","version":"1.0.0"},"instructions":"Use eval for persistent Smalltalk expressions, objects to find IDs, call to send typed messages, and inspect for state, active calls, placement and retention paths. Uncaught signals are tool errors."}`)
	}
	return response
}

func (c *Coordinator) callMCP(req MCPRequest) MCPResponse {
	response := MCPResponse{JSONRPC: "2.0", ID: req.ID}
	var call MCPToolCall
	var args MCPToolArguments
	if err := json.Unmarshal(req.Params, &call); err != nil || call.Name == "" {
		response.Error = &MCPError{-32602, "tools/call requires a tool name and arguments"}
	} else if err := decodeMCPArguments(call.Arguments, &args); err != nil {
		response.Error = &MCPError{-32602, err.Error()}
	} else if err := validateMCPArguments(call.Name, call.Arguments); err != nil {
		response.Error = &MCPError{-32602, err.Error()}
	} else {
		result, err := c.runMCPTool(call.Name, args)
		if err != nil {
			result = err.Error()
		} else {
			// Successful tool output is preserved as text, including JSON objects.
		}
		response.Result, _ = json.Marshal(MCPToolResult{Content: []MCPContent{{Type: "text", Text: result}}, IsError: err != nil})
	}
	return response
}

func validateMCPArguments(name string, raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	var required, optional []string
	switch name {
	case "eval": required = []string{"code"}
	case "inspect": required = []string{"objectId"}
	case "call": required, optional = []string{"objectId", "method"}, []string{"args"}
	case "objects", "metrics":
	default: return fmt.Errorf("unknown ServiceTalk tool %q", name)
	}
	if len(raw) == 0 {
		fields = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	} else {
		// Validate presence separately so an explicitly empty source stays valid.
	}
	for _, key := range required {
		if value, ok := fields[key]; !ok || bytes.Equal(value, []byte("null")) {
			return fmt.Errorf("%s requires %s", name, key)
		} else {
			delete(fields, key)
		}
	}
	for _, key := range optional {
		delete(fields, key)
	}
	for key := range fields {
		return fmt.Errorf("%s does not accept argument %s", name, key)
	}
	return nil
}

func decodeMCPArguments(raw json.RawMessage, args *MCPToolArguments) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	} else if raw[0] != '{' {
		return fmt.Errorf("tool arguments must be an object")
	} else {
		// Decode typed arguments and reject misspelled fields.
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(args); err != nil {
		return err
	} else {
		for index, arg := range args.Args {
			switch arg.Kind {
			case "number", "string", "objid", "bool", "nil":
			case "character":
				if !utf8.ValidRune(arg.Char) {
					return fmt.Errorf("argument %d has an invalid Unicode character", index)
				} else {
					// Character values must be Unicode scalar values.
				}
			default:
				return fmt.Errorf("argument %d has unsupported Value kind %q", index, arg.Kind)
			}
		}
		return nil
	}
}

func (c *Coordinator) runMCPTool(name string, args MCPToolArguments) (string, error) {
	switch name {
	case "eval":
		if id, err := c.ensureWorkspace(); err != nil {
			return "", err
		} else if value, err := c.callObjectID(id, "evalScript:", []Value{StrVal(args.Code)}, evalTimeout()); err != nil {
			return "", err
		} else {
			return value.Str, nil
		}
	case "objects":
		body, err := json.Marshal(c.objectSnapshot())
		return string(body), err
	case "inspect":
		if inspection, err := c.inspectObject(args.ObjectID, true); err != nil {
			return "", err
		} else {
			body, err := json.Marshal(inspection)
			return string(body), err
		}
	case "call":
		if args.ObjectID == "" || args.Method == "" {
			return "", fmt.Errorf("call requires objectId and method")
		} else if value, err := c.callObjectID(args.ObjectID, args.Method, args.Args, messageCallTimeout(args.Method, args.Args)); err != nil {
			return "", err
		} else {
			body, err := json.Marshal(value)
			return string(body), err
		}
	case "metrics":
		body, err := json.Marshal(MetricsResponse{RegistrySize: len(c.objectSnapshot()), RootCount: len(c.rootSnapshot()), WorkerCount: c.workerCount(), Runtime: runtimeMetricsSnapshot(), Coordinator: c.coordinatorMetricsSnapshot()})
		return string(body), err
	default:
		return "", fmt.Errorf("unknown ServiceTalk tool %q", name)
	}
}

func (c *Coordinator) workerCount() int {
	c.workerMu.RLock()
	defer c.workerMu.RUnlock()
	return len(c.workers)
}

func (c *Coordinator) objectSnapshot() []ObjectEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entries := make([]ObjectEntry, 0, len(c.registry))
	for _, entry := range c.registry {
		entries = append(entries, *entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

const mcpTools = `{"tools":[
{"name":"eval","description":"Evaluate Smalltalk or S-expressions in the persistent workspace. Can create objects and perform side effects. Uncaught signals fail the tool call.","inputSchema":{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}},
{"name":"objects","description":"List registered objects with IDs, class names and NATS subjects.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","properties":{},"additionalProperties":false}},
{"name":"inspect","description":"Inspect fields, methods, active calls, waits, host/PID/worker placement, and a sampled reference path from a GC root.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","properties":{"objectId":{"type":"string"}},"required":["objectId"],"additionalProperties":false}},
{"name":"call","description":"Send a method to an object. Arguments use ServiceTalk Value objects (kind and num/str/objid/bool/char). Can perform side effects.","inputSchema":{"type":"object","properties":{"objectId":{"type":"string"},"method":{"type":"string"},"args":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string","enum":["number","string","objid","bool","character","nil"]},"num":{"type":"number"},"str":{"type":"string"},"objid":{"type":"string"},"bool":{"type":"boolean"},"char":{"type":"integer","minimum":0,"maximum":1114111}},"required":["kind"],"additionalProperties":false}}},"required":["objectId","method"],"additionalProperties":false}},
{"name":"metrics","description":"Read coordinator spawn and pulse counts, errors and latency totals.","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","properties":{},"additionalProperties":false}}
]}`

// Keep the same upper bound as the existing inspector's NATS control calls.
const inspectionTimeout = 2 * time.Second
