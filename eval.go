package main

import (
	"fmt"
	"bytes"
	"encoding/json"
	"strings"
)

// EvalCtx holds the per-call evaluation context.
// When isBlock is true, variable reads/writes are proxied to the parent object.
// selfOverride, when non-nil, replaces the 'self' symbol — used for Number method dispatch.
type EvalCtx struct {
	obj          *ObjectServer
	args         map[string]Value // method params + captured block args
	isBlock      bool
	parentID     string // non-empty when isBlock == true
	selfOverride *Value // non-nil when running a Number.smt method
	// definingClass is the class whose method body is currently running.
	// A `super` send resumes method lookup from definingClass.Superclass.
	// nil at top level, in blocks, and for primitive dispatch.
	definingClass *ClassDef
}

// returnSignal is used by (return expr) / (^ expr) to implement early return.
// It is caught by a deferred recover() in the method dispatch code in object.go.
type returnSignal struct {
	value Value
}

// signalError is raised by (signal msg) and caught by (on:do:).
// Unhandled signals are caught in runBody and logged to the transcript.
type signalError struct {
	msg string
}

// ---- Evaluator ----

func (ctx *EvalCtx) eval(node *Node) Value {
	switch node.Kind {
	case NodeNumber:
		return NumVal(node.Num)
	case NodeString:
		return StrVal(node.Str)
	case NodeSymbol:
		return ctx.evalSymbol(node.Str)
	case NodeList:
		return ctx.evalList(node)
	}
	return Nil
}

func (ctx *EvalCtx) evalSymbol(name string) Value {
	switch name {
	case "nil":
		return Nil
	case "true":
		return True
	case "false":
		return False
	case "self":
		if ctx.selfOverride != nil {
			return *ctx.selfOverride
		}
		if ctx.isBlock {
			return ObjVal(ctx.parentID)
		}
		return ObjVal(ctx.obj.id)
	case "coordinator":
		// The coordinator is a first-class Smalltalk object with a fixed ID.
		// (send coordinator allObjects), (send coordinator size), etc.
		return ObjVal(coordinatorID)
	}

	if v, ok := ctx.args[name]; ok {
		return v
	}

	// Block: fetch instance var from parent via RPC (live reference)
	if ctx.isBlock {
		return ctx.obj.sendMessage(ObjVal(ctx.parentID), "getVar:", []Value{StrVal(name)})
	}

	ctx.obj.mu.RLock()
	v, ok := ctx.obj.instanceVars[name]
	ctx.obj.mu.RUnlock()
	if ok {
		return v
	}
	// Shell-mode fallback: when an objid selfOverride is in play (e.g.
	// `smt shell` set workspace.shellContext = a Shell), an unresolved
	// bare identifier is dispatched as a unary self-send.  Makes
	// `ls`, `pwd`, `jobs`, etc. work at the prompt without forcing
	// the user to write `self ls`.  Without selfOverride this stays
	// returning Nil — preserves the current "undefined identifier
	// reads as nil" semantics inside class methods.
	if ctx.selfOverride != nil && ctx.selfOverride.Kind == "objid" {
		if ctx.obj.sendMessage(*ctx.selfOverride, "respondsTo:", []Value{StrVal(name)}).Truthy() || ctx.obj.sendMessage(*ctx.selfOverride, "respondsTo:", []Value{StrVal("doesNotUnderstand:")}).Truthy() {
			return ctx.obj.sendMessage(*ctx.selfOverride, name, nil)
		} else {
			return Nil
		}
	}
	return Nil
}

func (ctx *EvalCtx) evalList(node *Node) Value {
	if len(node.Children) == 0 {
		return Nil
	}
	head := node.Children[0]
	if head.Kind != NodeSymbol {
		return ctx.eval(head)
	}

	switch head.Str {

	// ---- assignment ----
	case "set":
		return ctx.evalSet(node)

	// ---- early return ----
	case "return", "^":
		return ctx.evalReturn(node)

	// ---- control flow ----
	case "if":
		return ctx.evalIf(node)

	// ---- messaging ----
	case "send":
		return ctx.evalSend(node)
	case "new":
		return ctx.evalNew(node)
	case "block":
		return ctx.evalBlock(node)

	// ---- sequencing ----
	// (begin e1 e2 … en) — evaluate each in order, return the last.
	// The Smalltalk front end emits this for a cascade whose receiver
	// folds to a non-message form (e.g. a `Transcript show:` chain that
	// became a run of `(print …)` calls).
	case "begin":
		return ctx.evalBegin(node)

	// ---- cascade ----
	// (cascade recv (sel arg…) (sel2 arg…) …) — evaluate recv once,
	// send each message to it in turn, return the last message's value.
	case "cascade":
		return ctx.evalCascade(node)

	// ---- array literal ----
	// (array e1 e2 … en) — spawn an Array and `add:` each evaluated
	// element, returning the Array.  Emitted by the `{e. e. e}` and
	// `#(lit lit)` Smalltalk forms.
	case "array":
		return ctx.evalArrayLiteral(node)

	// ---- I/O ----
	case "print":
		return ctx.evalPrint(node)

	// ---- string concatenation ----
	// (concat a b c ...) — joins any number of values via Value.String().
	// Documented in README; the equivalent (send a , b , ...) chain is
	// awkward beyond two operands.
	case "concat":
		return ctx.evalConcat(node)

	// ---- JSON encoding ----
	// (jsonEncode val) — recursively renders a value as a JSON string.
	// Arrays and Dictionaries walk through child values via sendMessage,
	// so cross-worker structures encode correctly.  Used by HttpServer
	// handlers that want to return a Dictionary as the response body.
	case "jsonEncode":
		return ctx.evalJsonEncode(node)

	// (jsonDecode str) — parse a JSON string into a Value tree.  Objects
	// become Dictionaries, arrays become Arrays, primitives map to their
	// direct value kinds.  Returns nil on parse failure.  Inverse of
	// jsonEncode, used by HttpServer handlers reading request bodies.
	case "jsonDecode":
		return ctx.evalJsonDecode(node)

	// (objid "id") — objid literal.  Emitted by the Smalltalk parser
	// when it sees `#@id`.  No validation: any id string crosses the
	// boundary unchecked, mirroring how a captured ObjVal would.
	case "objid":
		if len(node.Children) < 2 || node.Children[1].Kind != NodeString {
			return Nil
		}
		return ObjVal(node.Children[1].Str)

	// (char N) — character literal from a Unicode code point.  Emitted by
	// the Smalltalk `$x` form; also writable directly in S-expression source.
	case "char":
		if len(node.Children) < 2 || node.Children[1].Kind != NodeNumber {
			return Nil
		}
		return CharVal(rune(int(node.Children[1].Num)))

	default:
		return ctx.evalSendToSelf(node)
	}
}

// resolveSelector interprets the selector + arguments of a message send.
// Given the slice whose first element is the selector symbol, it returns the
// method name and the list of argument expressions.
//
// If the slice matches a Smalltalk keyword pattern — two or more pairs of
// (single-colon keyword symbol, value) — the keywords are concatenated into
// one selector (`at: k put: v` → `at:put:`) and the values become the args.
// Otherwise the first element is used verbatim as the selector and the rest
// are the args (preserving unary, binary, and already-combined forms).
func resolveSelector(children []*Node) (string, []*Node) {
	if combined, argNodes, ok := combineKeywords(children); ok {
		return combined, argNodes
	}
	return children[0].Str, children[1:]
}

func combineKeywords(children []*Node) (string, []*Node, bool) {
	if len(children) < 4 || len(children)%2 != 0 {
		return "", nil, false
	}
	for i := 0; i < len(children); i += 2 {
		n := children[i]
		if n.Kind != NodeSymbol || !isSingleKeyword(n.Str) {
			return "", nil, false
		}
	}
	var sel strings.Builder
	args := make([]*Node, 0, len(children)/2)
	for i := 0; i < len(children); i += 2 {
		sel.WriteString(children[i].Str)
		args = append(args, children[i+1])
	}
	return sel.String(), args, true
}

func isSingleKeyword(s string) bool {
	return strings.HasSuffix(s, ":") && strings.Count(s, ":") == 1
}

// ---- Special forms ----

// (set varName expr)   /   varName := expr  in Smalltalk
//
// Writes a declared instance variable.  Auto-create is gone — the
// pulse-GC walks instanceVars to keep refs alive, and writing to
// undeclared fields makes it impossible for the author to know which
// state is intentional vs. a typo.  Inside a block, the write
// forwards to the parent object via RPC; the parent's `setVar:to:`
// performs the same declaration check there.
//
// The workspace (`__workspace__`) is exempt — it's the persistent
// REPL state and accepts any name, since interactive sessions don't
// declare vars up front.
func (ctx *EvalCtx) evalSet(node *Node) Value {
	if len(node.Children) < 3 || node.Children[1].Kind != NodeSymbol {
		ctx.obj.transcriptf("set: bad syntax at line %d", node.Line)
		return Nil
	}
	name := node.Children[1].Str
	val := ctx.eval(node.Children[2])

	if ctx.isBlock {
		ctx.obj.sendMessage(ObjVal(ctx.parentID), "setVar:to:", []Value{StrVal(name), val})
		return val
	}

	if !classAllowsSetVar(ctx.obj.classDef, name) {
		ctx.obj.transcriptf("set: undeclared field %q on class %s (line %d); declared fields: %v — add %q to the class's vars list",
			name, ctx.obj.classDef.Name, node.Line, ctx.obj.classDef.Vars, name)
		return Nil
	}

	ctx.obj.mu.Lock()
	ctx.obj.instanceVars[name] = val
	ctx.obj.mu.Unlock()
	return val
}

// classAllowsSetVar returns true when `name` is a writable field on the
// given class or any of its superclasses.  The workspace accepts any
// name; every other class requires the field to appear in its
// `(vars …)` / `| … |` declaration somewhere up the inheritance chain.
func classAllowsSetVar(cd *ClassDef, name string) bool {
	if cd == nil {
		return false
	}
	if cd.Name == BuiltinClassWorkspace {
		return true
	}
	for c := cd; c != nil; c = c.Superclass {
		for _, v := range c.Vars {
			if v == name {
				return true
			}
		}
	}
	return false
}

// (return expr) / (^ expr)  — early return from the enclosing method.
// The returnSignal is caught by the deferred recover in callMethod (object.go).
func (ctx *EvalCtx) evalReturn(node *Node) Value {
	var val Value
	if len(node.Children) > 1 {
		val = ctx.eval(node.Children[1])
	}
	panic(returnSignal{value: val})
}

// (concat a b c ...) — concatenate any number of values rendered via
// Value.String().  No args yields an empty string; one arg is rendered
// directly.
func (ctx *EvalCtx) evalConcat(node *Node) Value {
	var b strings.Builder
	for _, c := range node.Children[1:] {
		b.WriteString(ctx.eval(c).String())
	}
	return StrVal(b.String())
}

// (jsonDecode str) — parse a JSON string into a Value tree.
//
//	null      → nil
//	bool      → bool
//	number    → number
//	string    → string
//	array     → freshly-spawned Array, with child values recursively decoded
//	object    → freshly-spawned Dictionary, ditto
//
// Returns nil on parse failure (the request handler can spot this and
// reply 400).  Side-effecting because nested arrays/objects spawn
// processes; do not call inside a hot inner loop.
func (ctx *EvalCtx) evalJsonDecode(node *Node) Value {
	if len(node.Children) < 2 {
		return Nil
	}
	return decodeJSONString(ctx.obj, ctx.eval(node.Children[1]).String())
}

// (jsonEncode val) — recursively render val as a JSON string.
//
//	nil           → "null"
//	bool          → "true" / "false"
//	number        → integer or float literal
//	string        → JSON-quoted, with \, ", and control chars escaped
//	Array objid   → JSON array of recursively-encoded elements
//	Dict objid    → JSON object with string keys and recursively-encoded
//	                values
//	any other objid → "null" (user classes can implement their own
//	                  serialisation by returning a Dictionary from a
//	                  toJsonish method, etc.)
//
// Cycles are guarded by the seen-set; a cycle short-circuits to "null"
// rather than looping forever.
func (ctx *EvalCtx) evalJsonEncode(node *Node) Value {
	if len(node.Children) < 2 {
		return StrVal("null")
	}
	v := ctx.eval(node.Children[1])
	return StrVal(encodeJSON(ctx.obj, v, map[string]bool{}))
}

// (if cond then [else])
func (ctx *EvalCtx) evalIf(node *Node) Value {
	if len(node.Children) < 3 {
		return Nil
	}
	if ctx.eval(node.Children[1]).Truthy() {
		return ctx.eval(node.Children[2])
	}
	if len(node.Children) >= 4 {
		return ctx.eval(node.Children[3])
	}
	return Nil
}

// (send target method arg ...)
func (ctx *EvalCtx) evalSend(node *Node) Value {
	if len(node.Children) < 3 {
		ctx.obj.transcriptf("send: too few arguments at line %d", node.Line)
		return Nil
	}
	if node.Children[2].Kind != NodeSymbol {
		ctx.obj.transcriptf("send: method must be a symbol at line %d", node.Line)
		return Nil
	}
	method, argExprs := resolveSelector(node.Children[2:])
	args := make([]Value, 0, len(argExprs))
	for _, c := range argExprs {
		args = append(args, ctx.eval(c))
	}
	// `super sel ...` — dispatch locally, resuming lookup above the class
	// that defined the running method.  Detected before evaluating the
	// receiver since `super` is not itself a value.
	if recv := node.Children[1]; recv.Kind == NodeSymbol && recv.Str == "super" {
		return ctx.evalSuperSend(method, args)
	}
	target := ctx.eval(node.Children[1])
	return ctx.dispatchValue(target, method, args)
}

// evalSuperSend handles `super sel ...`.  The receiver is still self, but
// method lookup starts from the superclass of the class whose body is
// running — so an override can delegate to the version it shadows.
func (ctx *EvalCtx) evalSuperSend(method string, args []Value) Value {
	if ctx.definingClass == nil {
		panic(signalError{msg: fmt.Sprintf("super send %q requires a method with a defining class", method)})
	} else {
		return ctx.obj.callMethodFrom(ctx.definingClass.Superclass, method, args)
	}
}

// dispatchValue routes a message to a target value: primitives (number,
// string, bool, nil) dispatch locally through their callXMethod handlers,
// everything else (objids) goes over the wire via sendMessage.  Shared by
// evalSend and evalCascade so both honour the same primitive shortcuts.
func (ctx *EvalCtx) dispatchValue(target Value, method string, args []Value) Value {
	switch target.Kind {
	case "number":
		return callNumberMethod(ctx.obj, target.Num, method, args)
	case "string":
		return callStringMethod(ctx.obj, target.Str, method, args)
	case "bool":
		return callBoolMethod(ctx.obj, target.Bool, method, args)
	case "character":
		return callCharacterMethod(ctx.obj, target.Char, method, args)
	case "nil", "":
		return callNilMethod(ctx.obj, method, args)
	}
	return ctx.obj.sendMessage(target, method, args)
}

// (begin e1 e2 … en) — evaluate each child in order, return the last
// (or nil when empty).
func (ctx *EvalCtx) evalBegin(node *Node) Value {
	var result Value = Nil
	for _, c := range node.Children[1:] {
		result = ctx.eval(c)
	}
	return result
}

// (cascade recv (sel arg…) …) — evaluate recv once, then send each
// message to that same value in turn.  Returns the last message's
// result, matching Smalltalk cascade semantics.  Each message child is
// a list whose head symbol is the (already-combined) selector and whose
// tail are the argument expressions.
func (ctx *EvalCtx) evalCascade(node *Node) Value {
	if len(node.Children) < 2 {
		return Nil
	}
	recv := ctx.eval(node.Children[1])
	result := recv
	for _, msg := range node.Children[2:] {
		if msg.Kind != NodeList || len(msg.Children) == 0 || msg.Children[0].Kind != NodeSymbol {
			continue
		}
		selector := msg.Children[0].Str
		args := make([]Value, 0, len(msg.Children)-1)
		for _, a := range msg.Children[1:] {
			args = append(args, ctx.eval(a))
		}
		result = ctx.dispatchValue(recv, selector, args)
	}
	return result
}

// (array e1 … en) — spawn a fresh Array and add: each evaluated element,
// returning the Array's objid.  Empty `{}` / `#()` yields an empty Array.
func (ctx *EvalCtx) evalArrayLiteral(node *Node) Value {
	arr, ok := ctx.spawnInstance(UserClassArray)
	if !ok {
		return Nil
	}
	for _, c := range node.Children[1:] {
		ctx.obj.sendMessage(arr, "add:", []Value{ctx.eval(c)})
	}
	return arr
}

// (methodName arg ...)  — implicit send to self (or parent if in a block)
func (ctx *EvalCtx) evalSendToSelf(node *Node) Value {
	method, argExprs := resolveSelector(node.Children)
	args := make([]Value, 0, len(argExprs))
	for _, c := range argExprs {
		args = append(args, ctx.eval(c))
	}
	// In a Number.smt method, self is the number — route back through number dispatch.
	if ctx.selfOverride != nil && ctx.selfOverride.Kind == "number" {
		return callNumberMethod(ctx.obj, ctx.selfOverride.Num, method, args)
	}
	if ctx.selfOverride != nil && ctx.selfOverride.Kind == "string" {
		return callStringMethod(ctx.obj, ctx.selfOverride.Str, method, args)
	}
	if ctx.selfOverride != nil && ctx.selfOverride.Kind == "bool" {
		return callBoolMethod(ctx.obj, ctx.selfOverride.Bool, method, args)
	}
	// Shell-mode: an objid selfOverride means "self is some other
	// object" — route the implicit self-send to that object instead
	// of the workspace itself.
	if ctx.selfOverride != nil && ctx.selfOverride.Kind == "objid" {
		return ctx.obj.sendMessage(*ctx.selfOverride, method, args)
	}
	self := ObjVal(ctx.obj.id)
	if ctx.isBlock {
		self = ObjVal(ctx.parentID)
	}
	return ctx.obj.sendMessage(self, method, args)
}

// (new ClassName arg ...)
func (ctx *EvalCtx) evalNew(node *Node) Value {
	if len(node.Children) < 2 || node.Children[1].Kind != NodeSymbol {
		ctx.obj.transcriptf("new: class name required at line %d", node.Line)
		return Nil
	}
	className := node.Children[1].Str
	initArgs := make([]Value, 0, len(node.Children)-2)
	for _, c := range node.Children[2:] {
		initArgs = append(initArgs, ctx.eval(c))
	}

	newObj, ok := ctx.spawnInstance(className)
	if !ok {
		return Nil
	}
	ctx.obj.sendMessage(newObj, "initialize", initArgs)
	return newObj
}

// spawnInstance asks the coordinator to spawn a fresh instance of
// className, records it in the local registry cache, and returns its
// objid.  It does NOT run initialize — callers that need it (evalNew)
// send the message themselves; callers building built-ins (array
// literals) skip it.  Returns ok=false after logging on spawn failure.
func (ctx *EvalCtx) spawnInstance(className string) (Value, bool) {
	reqBody, _ := json.Marshal(SpawnRequest{ClassName: className})
	resp, err := rpcClient.Post(ctx.obj.coordinatorURL+"/spawn", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		panic(signalError{msg: fmt.Sprintf("new %s: spawn request to %s failed: %v", className, ctx.obj.coordinatorURL, err)})
	}
	defer resp.Body.Close()

	var spawnResp SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&spawnResp); err != nil {
		panic(signalError{msg: fmt.Sprintf("new %s: invalid spawn response from %s: %v", className, ctx.obj.coordinatorURL, err)})
	}
	if spawnResp.Error != "" {
		panic(signalError{msg: fmt.Sprintf("new %s: spawn failed: %s", className, spawnResp.Error)})
	}

	newID := spawnResp.ObjectID
	ctx.obj.registry.Store(newID, ObjectEntry{
		ID:        newID,
		Subject:   spawnResp.Subject,
		ClassName: className,
	})
	return ObjVal(newID), true
}

// (block (params) body...)
// Serialises the block closure to a temp file and spawns a __block__ process.
func (ctx *EvalCtx) evalBlock(node *Node) Value {
	if len(node.Children) < 3 || node.Children[1].Kind != NodeList {
		ctx.obj.transcriptf("block: expected (block (params) body...) at line %d", node.Line)
		return Nil
	}

	var params []string
	for _, p := range node.Children[1].Children {
		if p.Kind == NodeSymbol {
			params = append(params, p.Str)
		}
	}
	body := node.Children[2:]

	// Snapshot current method args (values, not AST — safe to copy)
	captured := make(map[string]Value, len(ctx.args))
	for k, v := range ctx.args {
		captured[k] = v
	}

	// Nested blocks still refer to the original parent, not this block's process
	parentID := ctx.obj.id
	if ctx.isBlock {
		parentID = ctx.parentID
	}

	// Propagate REPL capture: if this eval is running inside a workspace eval:,
	// give the block the workspace objid so it can forward (print ...) output.
	// Nested blocks (blocks spawned from blocks) inherit the capture object.
	captureObjectID := ""
	ctx.obj.evalCaptureMu.Lock()
	if ctx.obj.evalCapturing {
		captureObjectID = ctx.obj.id
	}
	ctx.obj.evalCaptureMu.Unlock()
	if captureObjectID == "" && ctx.obj.blockDef != nil {
		captureObjectID = ctx.obj.blockDef.CaptureObjectID
	}

	bd := &BlockDef{
		Params:          params,
		Body:            body,
		ParentID:        parentID,
		CapturedArgs:    captured,
		CaptureObjectID: captureObjectID,
		SelfOverride: ctx.selfOverride,
	}

	reqBody, _ := json.Marshal(SpawnRequest{ClassName: BuiltinClassBlock, BlockDef: bd})
	resp, err := rpcClient.Post(ctx.obj.coordinatorURL+"/spawn", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		ctx.obj.transcriptf("block: spawn failed: %v", err)
		return Nil
	}
	defer resp.Body.Close()

	var spawnResp SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&spawnResp); err != nil {
		ctx.obj.transcriptf("block: spawn response decode error: %v", err)
		return Nil
	}
	if spawnResp.Error != "" {
		ctx.obj.transcriptf("block: spawn error: %s", spawnResp.Error)
		return Nil
	}

	ctx.obj.registry.Store(spawnResp.ObjectID, ObjectEntry{
		ID:        spawnResp.ObjectID,
		Subject:   spawnResp.Subject,
		ClassName: BuiltinClassBlock,
	})

	return ObjVal(spawnResp.ObjectID)
}

// (print expr ...)
func (ctx *EvalCtx) evalPrint(node *Node) Value {
	parts := make([]string, 0, len(node.Children)-1)
	for _, c := range node.Children[1:] {
		parts = append(parts, ctx.eval(c).String())
	}
	text := strings.Join(parts, " ")
	ctx.obj.transcriptf("%s", text)
	return StrVal(text)
}
