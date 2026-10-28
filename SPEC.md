# ServiceTalk runtime contract

ServiceTalk evaluates Smalltalk syntax in cooperating object processes. The
coordinator owns the registry and reachability roots; workers can host objects;
NATS carries calls and replies. The existing README is the detailed language,
object-model, foreign-class, packaging and deployment reference. This spec
records the repaired executable-file lifecycle and closure reachability
contract; it does not replace the rest of that reference.

## Executable files and application directories

- `smt run DIR [flags]` retains application-directory semantics: put DIR on the
  classpath and start Main (or the requested main class) as a serving app.
- `smt script FILE` evaluates one file of Smalltalk workspace expressions and
  exits. The suffix does not change parsing. Missing/unreadable files and syntax
  errors are reported on stderr with the filename and a nonzero exit code.
  A file may be empty; its result is nil. Relative paths resolve from the caller's
  working directory. The script directory is added to the child classpath so
  sibling `.st`/`.smt` class definitions remain available.
- `smt test FILE...` runs each named executable file in order. Without names it
  discovers `*_test.smt`, then `*Test.smt`, in the current directory. No matches
  is an error. Each file has an independent runtime/workspace. A failure does
  not stop subsequent files. Output includes `ok` or `FAIL` per file and a final
  passed/failed count; any failed file makes the command exit nonzero.
- The test command is an executable-script runner, not a new assertion language.
  A successful script returns zero regardless of its final value; signal an
  error to fail it. Uncaught signals propagate through native methods, blocks,
  parallel sends and foreign methods to the workspace boundary. A handled signal
  succeeds normally. Execution after an uncaught signal is skipped.
- `coordinator --script FILE` is the child launcher form. It cannot be combined
  with `--eval` or `--repl`. It uses the same file checks and strict workspace
  evaluation; startup/evaluation errors produce a nonzero exit status after
  child shutdown. A caller invoking this lower-level form supplies its broker.

## Isolation, deadline and cleanup

The script runner launches its own coordinator subprocess and embedded NATS
broker. It clears inherited broker URL, cluster, registry-file and advertised
host settings, chooses a unique namespace, and advertises loopback. The script
coordinator binds loopback, with port zero requesting a port assigned by the
operating system. The bound port is discovered before objects are spawned; no
probe/close/rebind port selection is used. No parent workspace or persisted
registry is adopted. Ordinary serving modes retain their existing bind behavior.

The runner waits for the child rather than starting a detached coordinator in
its own process. Its deadline is the sum of configured NATS startup, spawn and
evaluation timeouts plus ten seconds for cleanup, capped at 24 hours. This is an
outer lifecycle bound, not receiver-side cooperative cancellation. The normal
one-shot path requests child-object shutdown on success and error. On Unix the
runner gives the child a process group and kills remaining group members when
the coordinator exits or the deadline fires; descendants deliberately starting
new sessions are outside this boundary. Windows bounds the direct coordinator
and inherited I/O waits; complete descendant containment requires a job object
and is not provided in this change.

## Evaluation protocol

The browser workspace initially displays a runnable Smalltalk counter example:
create `c := Counter new`, increment twice, add ten, then read `c value`.
Statements are separated by periods and the introductory comment uses double
quotes. Pressing Eval or Ctrl+Enter runs the example and displays `12`.

`MessageRequest` retains its existing JSON shape: message identifier, optional
call-chain identifier, method selector and Value arguments. The workspace now
accepts `evalScript:` as well as `eval:`. Both take source text and produce the
same captured-output/result formatting. `evalScript:` propagates parse errors
and uncaught signals through the existing signaled `ReplyRequest`;
the one-shot coordinator reports these as errors. Interactive `eval:` retains
transcript capture; uncaught execution signals now reach the HTTP error field.
No `EvalResponse.Failures` field exists or
is introduced. The outer script runner uses the child exit status.

## Closure reachability

`BlockDef` contains parameters, body AST, ParentID, captured argument values and
optional transcript capture object ID. ParentID is the lexical owner used by
block evaluation to access variables. A live block must therefore forward every
new GC pulse to a nonempty ParentID in addition to object references in captured
arguments and ordinary object state. An escaped block keeps its owner alive;
cycles still terminate through the existing monotonically increasing sweep-ID
check. An empty ParentID adds no target. This change does not redesign in-flight
reference accounting or foreign-language reference tracking.

## Validation contract

The clean source tree must build without overlays. Local suites exercise the
real CLI through a subprocess, isolated embedded brokers, script success,
empty input, syntax/signal failure, sibling classes, continued batch execution,
and background-child cleanup on Unix. Pulse tests cover both the reference set
and actual NATS propagation from an escaped block to its lexical owner. Full
unit/integration and race tests remain required.

## MCP interface

Every coordinator serves `/mcp` alongside the browser interface. Clients use
Streamable HTTP JSON-RPC with protocol version `2025-06-18`. `initialize`
returns server information and the tools capability; `ping` returns an empty
object; `tools/list` describes available tools and their argument schemas.
Accepted notifications receive HTTP 202 with no body. Requests receive one
JSON response. GET returns 405 because no SSE stream is offered. An Origin
header, when present, must match the coordinator host. Unsupported explicit
protocol versions are rejected. Requests are bounded by `SMT_NATS_MAX_CALL_BYTES`.
Oversized bodies return HTTP 413. Invalid JSON/UTF-8 and invalid JSON-RPC
envelopes are rejected. Request IDs must be strings or integer numbers;
explicit null IDs and non-object params are invalid. Initialization requires
protocolVersion, a capabilities object and clientInfo with name/version.
If a client proposes another version, the server offers its supported version.
This endpoint does not introduce an authentication system.

Tool calls use `tools/call` with a name and argument object:

- `eval` requires `code`, evaluates it with `evalScript:` in the persistent
  workspace, and returns captured output/result text. The same workspace is
  shared with the browser REPL. Evaluation may perform side effects.
- `objects` takes no arguments and returns registry entries sorted by ID.
- `inspect` requires `objectId` and returns fields, methods, runtime activity,
  placement and retention information as JSON text.
- `call` requires `objectId` and `method`, with optional `args` containing
  ordinary ServiceTalk Value objects. It returns the result Value as JSON text.
- `metrics` takes no arguments and returns registry/root/worker counts, local
  runtime counters and coordinator spawn/pulse counters.

Protocol errors use JSON-RPC error objects. Tool execution failures return
text content with `isError: true`, including nested and foreign signals.
Successful calls return `isError: false`. Required arguments and unsupported
argument names are checked. The client may use the coordinator URL directly,
for example `http://127.0.0.1:8000/mcp`; no temporary script files are needed.
For Codex, register a running coordinator with
`codex mcp add servicetalk --url http://127.0.0.1:8000/mcp`, then verify it with
`codex mcp get servicetalk`. Use the actual coordinator port in the URL.
This follows the [official MCP configuration guidance](https://developers.openai.com/learn/docs-mcp).
The `call` schema describes every Value field and valid kind. Unknown kinds,
missing kinds and invalid Unicode character values are rejected before dispatch.
Read-only tools (`objects`, `inspect`, `metrics`) advertise readOnlyHint.

`MCPRequest` contains JSON-RPC version, raw JSON request ID, method and raw
params. `MCPResponse` echoes the ID and contains raw result or `MCPError`
(code/message). `MCPToolCall` carries name and argument JSON. Typed tool
arguments contain source, object ID, selector and Value arguments.
`MCPToolResult` contains text content entries and an error boolean.
`MCPInitializeParams` carries protocolVersion, raw capabilities and typed
clientInfo; `MCPClientInfo` contains the client's name and version.

### Client interoperability check

`examples/mcp` contains a runnable client using the official MCP TypeScript SDK,
with a pinned dependency and lockfile. After starting a coordinator, install
the client with `npm ci --prefix examples/mcp`, then run
`npm test --prefix examples/mcp -- http://127.0.0.1:8000/mcp`.
The default endpoint is `http://127.0.0.1:8000/mcp` when omitted.
The check negotiates the protocol, verifies tool discovery, creates an Array,
checks workspace persistence and typed calls, inspects its root path and process
identity, reads metrics, and checks nested/handled errors. Its unique workspace
variable is cleared before disconnecting so pulse GC can reclaim the objects.
Failures exit nonzero; success prints a JSON result with server and tool names.
No application scripts or generated class files are required by this check.

## Runtime inspection

`InspectResponse` adds optional runtime and retention records. Runtime records
contain hostname, process ID, worker ID (empty for coordinator-hosted objects),
active calls, outbound waits, visible object references, whether a foreign
bridge is present, pulse timeout, last sweep ID, local pulse age and mailbox
metrics. Worker children receive `SMT_WORKER_ID` from their spawning worker.
Each active-call record contains call ID, chain ID, method, target object,
state (`executing` or `waiting`) and start time. Records are removed on return
or signal. Diagnostic control calls do not appear as application activity.

Retention records contain root membership, a sampled path from a root, IDs
that could not be inspected, and a note explaining the snapshot boundary.
The coordinator walks visible references from sorted roots, deduplicates
visited IDs and bounds traversal time. A missing path is not a proof that an
object is unreachable: foreign-held and in-transit references may be hidden.

Clicking a Live Objects row opens the existing inspector. Above variables and
methods it now displays host/PID, worker placement, mailbox depth, root/path
information, unavailable objects, foreign-reference context and active calls
with target, start time and chain. Long text wraps. Escape or clicking the
backdrop closes the inspector. Each opening samples current runtime state.

## Recovery behavior

Pulse lease expiry uses the object's local arrival time, preserving the
monotonic component of its clock. Remote sweep IDs only order propagation and
break cycles. A newly started object has a local lease and has not accepted
any remote sweep. Duplicate/older sweeps do not renew the lease. Coordinator
clock offsets cannot immediately expire a fresh object or grant extra lifetime.

A failed stale-object probe starts a five-second suspicion window. Further
failures during that window do not evict the object. A successful probe clears
suspicion; repeated failure after the window permits eviction. This preserves
live workspaces across brief broker outages without restoring dead object state.

Worker heartbeat tests cover re-registration after coordinator replacement.
Real-process MCP tests kill a worker and verify that its already-spawned,
reachable objects still answer: pulses come from the coordinator, so worker
death alone does not imply object death. Broker-outage tests verify failure
reporting and reconnection with the same object state. C integration tests
verify signal propagation and explicit pulse forwarding to a C-held reference.

## Timeout follow-up design

The intended next change is to carry a deadline through the call chain, reject
expired queued calls, and check cancellation at evaluator boundaries and before
outbound calls. Native/foreign blocking operations require their own cancellation
hooks. Cancellation must not trigger implicit retries or imply rollback of
side effects. This is a plan, not a new wire or execution guarantee in this change.

## Git component

`Git.st` provides a loadable `Git` object backed by `Process` using explicit
argument arrays. It invokes the installed `git` executable directly; filenames
and commit messages are never interpolated into shell commands. Git's own hooks
and configured helpers retain normal Git behavior.
`directory: path` selects the repository (default current directory).
`run: arguments` waits and returns the Process even on nonzero exit;
`lastProcess` exposes its output, status and exit code. `checked: arguments`
returns output on zero exit and signals command, directory, exit code and
captured output otherwise. Process output is merged and capped at 1 MiB.
Convenience methods are `status`, `diff`, `stagedDiff`, `head`, `branch`,
`remotes`, `log`, `fetch: remote`, `add: paths` (an Array),
`commit: message`, and `push: remote branch: name`. Add and push use an
end-of-options delimiter. Push never adds force; commit uses the existing index.
Methods run on the object's host with its existing Git credentials/configuration.
For MCP use: `repo := Git new. repo directory: '/path/to/repository'. repo status`.
The object persists, so later eval calls can stage, inspect, commit and push.
`examples/mcp/git-smoke.mjs` checks these operations through MCP using temporary
working and bare repositories, including literal shell metacharacters, filenames
beginning with a dash, multiline commit messages and nonzero/startup failures.
Run it with Node and the coordinator MCP URL after installing the example's SDK
dependencies. Temporary repositories and the workspace variable are cleaned up.

The browser header shows only the Ctrl+Enter shortcut. The inspector omits
the explanatory retention note from its display.

Live Objects displays Class and ID. Selecting an object opens its inspector.
Method buttons fill the method selector; keyword methods show one typed input
per argument, and binary methods show one. Send invokes the object through MCP
call, displays the returned value or error, and refreshes its fields. Argument
types are number, string, Boolean, nil, object ID and character. Object references
in fields and results are links to their inspectors. The modal fits narrow screens.

Inspector methods are sorted alphabetically on every refresh. Clicking a unary
method immediately invokes it and displays the result; methods with arguments
open the argument inputs and use Send. Method clicks are ignored during a call.

Git authentication is configurable per object. `useSSH` enables noninteractive
OpenSSH (BatchMode=yes); `sshIdentityFile:`, `sshAgent:` and `sshConfigFile:`
select an existing identity file, agent socket and SSH configuration file. Empty
strings clear each override. `sshCommand` reports the command without reading
key material; paths are individually quoted for Git’s SSH command handling.
`useDefaultAuth` returns to Git’s configured authentication. Overrides apply
only to this object’s calls and are not written to global Git configuration.
`remote: name url: url` and `remote: name pushURL: url` persist repository remote
URLs. `checkAuth: remote` probes access using ls-remote and reports Git failures.
SSH host verification remains enabled. Existing keys/agents supply credentials.

## Inspector signatures and properties

Inspection returns methodSignatures mapping each native selector to its parameter
names, including inherited methods with subclass overrides. Existing methods
and instanceVars fields remain compatible. The UI uses these names for inputs.
An optional ordinary method inspectorMetadata returns a JSON string containing
methods and properties. methods maps selectors to records with arguments
([{name,type}]) and optional returns. properties is an array of
{name,getter,setter?,type?}. Types are number, string, bool, nil, objid and char.
This metadata describes the UI; it does not impose language type restrictions.
All methods remain callable and visible.
The browser calls inspectorMetadata and each declared getter when opening or
refreshing an inspector. Getters are ordinary calls and should be written for
reading values. A getter-only property is displayed read-only. A setter adds an
input and Set button; Enter also applies the edit. Set invokes the setter with
one typed Value, then rereads every property and the object fields. Computed
properties therefore update alongside stored ones. Errors are shown without
writing raw instance state. Missing metadata retains the existing inspector.
Counter exposes its count through value/count:. Thermometer exposes celsius
and computed fahrenheit with setters that reject temperatures below absolute
zero. Instantiate it with `thermometer := Thermometer new`. Metadata travels
with class source and works with ordinary class loading and embedded builds.
The method form displays optional return-type metadata beside its argument inputs.

## Method navigation and relationship graphs

The header offers Browse methods and Object graph. Both open a resizable-to-viewport browser with Methods/Objects switches, class/selector search, Fit graph, Refresh and Close. Escape closes it. Method search lists definitions alphabetically; an exact selector lists its implementors and syntactic senders. Selecting a definition opens numbered class source and highlights the first body line; outgoing selector buttons navigate to their implementors/senders. Foreign methods open their manifest declarations. Refresh rereads the coordinator classpath and embedded sources, respecting loader precedence. Source is the current definition on disk, not a claim about code already running in remote processes. Native AST sends and cascades are indexed, including nested blocks; dynamic dispatch targets and foreign-language bodies are not inferred.

GET /navigation returns NavigationIndex: methods and errors. Each NavigationMethod contains class, selector, parameter names, sorted sends, full source, file, line, and foreign flag. A malformed class produces a per-file error while other classes remain browsable. Embedded source has an embedded: filename. GET /relationships?id=ID returns InspectResponse with current runtime references and root membership, without repeating a root-path traversal. Both endpoints require GET.

The Methods graph connects definition boxes to selector boxes with implements and sends edges. The Objects graph samples registered objects with six concurrent readers; failures appear individually. Named fields, collection entries and captured arguments label reference edges. Block (parent) fields are lexical-owner edges. Runtime references without names are reference edges. Roots, host/worker placement, and active outbound calls have distinct labelled edges. References do not imply exclusive ownership. Foreign state appears only when exposed by runtime inspection. Object boxes open the interactive inspector, and method/selector boxes navigate source and definitions.

Graphs use an SVG spring layout with repulsion and box separation, directed arrows, and edge colours. Drag boxes to position them; drag the background to pan; scroll to zoom around the cursor; Fit graph fits all boxes. Layout stops after settling and is cancelled on close or replacement. The detail pane scrolls independently, source scrolls horizontally within its pane, and narrow windows stack graph and details vertically. Inspector Browse methods and Object graph buttons link into the same browser without sending a method.

## Assisted object workspaces

Each inspector has an Object workspace section. Its receiver label identifies self. Read-property buttons run the declared getter. The method builder lists sorted available selectors and supplies visible argument names, type choices and optional return-type hints from inspector metadata. Argument choices also include expression, for it or another Smalltalk expression. Insert method inserts a complete self-send into the editor selection, replacing the initial self placeholder. It validates literal arguments and quotes strings; it does not execute. Run expression and Ctrl+Enter evaluate the editor, display transcript text and the typed result as it, then refresh the inspector's properties and fields. Object results link to their inspectors. Draft source and displayed results stay separate per receiver in the browser tab.

ObjectWorkspace.smt is an ordinary loadable class with target and workspace fields. initialize creates a native workspace. target: binds the receiver through its shellContext; target reads it; evaluate: delegates to evalResult:; result returns the last successful it; bindings returns the workspace object for inspection. It can be created and used directly over MCP. Scratch assignments belong to this workspace, not the receiver. self refers to the receiver, and unresolved bare names follow the existing shell-context unary-send rule.

Native workspace evalResult: accepts Smalltalk source and returns a JSON string encoding WorkspaceResult {result: Value, output: string, error?: string}. It propagates parse/runtime signals, captures printed output, stores the successful typed result in it, and never invokes printString on that result. Failed evaluations leave the prior it in place (earlier side effects remain ordinary side effects). Existing eval:/evalScript: response formats are unchanged. initialize is a no-op returning the native workspace.

POST /workspace accepts ObjectWorkspaceRequest {objectId, code}. It confirms receiver availability, creates or reuses an ObjectWorkspace rooted under __inspectorWorkspace_ plus hex-encoded receiver ID in the main workspace, then evaluates the source. Workspaces and bindings last for the coordinator session and remain when inspectors close. The endpoint returns WorkspaceResult with an error on failure. Request bodies use the existing size bound and evaluation timeout. Internal source generation uses Smalltalk syntax, validates object-ID characters and doubles string quotes.

BlockDef gains optional selfOverride (Value). Block creation captures a bound receiver; nested block execution restores it, while the lexical workspace remains the parent for scratch bindings. Receiver object references participate in pulse forwarding along with lexical owners and captured arguments. This also preserves existing shell-bound self inside closures.

## Reusable interactive results

Main REPL evaluations, explicit inspector sends/setters, object-workspace expressions and custom-view actions use POST /interaction with InteractionRequest {kind: eval|call|workspace, objectId?, method?, args?: Value[], code?, recordingId?}. InteractionResult contains result (Value), output, handle, error and optional recordingError. Method argument validation uses the MCP validator. Each successful result is retained as result_<unique ID> in the main workspace, so result objects remain reachable even after it changes. Infrastructure uses evalInternal:, a serialized protected workspace evaluation returning the raw Value without printString, changing it, or applying shellContext.

Output preserves typed values and object identity. Object references open inspectors. Use in REPL inserts the handle; Name this result and Bind create an ordinary named workspace binding; Pick as argument selects a value for a method argument's result type. Result-typed inputs offer retained results in a datalist. The Results button opens all retained interaction results, including custom-view actions whose views have since refreshed. Release removes the handle's reference; user-created named bindings remain. Clear releases the tab's handles and clears output. POST /result accepts action bind|release, handle and optional name. Names are validated identifiers excluding reserved words and internal prefixes. Characters use the wire kind character.

## Object-defined views

An ordinary inspectorViews method returns a JSON string {views:[{id,title,body}]}. The inspector displays tabs in declared order. Each body is a tree of controls with kind: column, row, value, text, code, input, slider, textarea, action, table or list. Nodes may have label, getter, args and children. Text/code may use literal text. Inputs use getter, setter and a Value type; sliders also have min/max/step. Action uses method, args, text and optional refresh. Each live control reads, writes or invokes through its own receiver, inherited from its containing layout/view when unset. Explicit actions are recorded; automatic rendering reads are not.

Table/list getters return a JSON array string. Table columns are {key,label}; without columns they are inferred from the first row. Scalar cells render as text; Value cells preserve types and object links. Tables scroll within their panel; code wraps; rows wrap on narrow windows. Errors appear within the affected control. Inputs submit with Apply or Enter (textarea retains Enter for editing). All dynamic content is rendered as text or structured controls.

Every displayed view is an ordinary InspectorView object with target, definition metadata and root fields, target/target:, root/root: and definition/definition: methods. Its definition property is editable through the inspector. POST /view accepts {target,key,definition}, creates/reuses a rooted view object and returns {objectId,definition,error?}. Existing live definitions win over class defaults. Inspect view object opens that object. Edit layout opens a visual designer; Save layout sends definition: and rerenders the receiver. Reload saved layout discards the draft and reads the stored definition. These instances live for the runtime session. Counter supplies a counter panel; Thermometer supplies temperature controls and a computed Readings table; Git supplies directory editing and Status/Diff/Log actions.

## Interaction recording and executable export

Record starts a rooted InteractionLog object; Stop recording stops appending and opens the recording. Recording reopens the most recent log. InteractionLog holds records and retained references as Arrays, exposes append:, retain:, entries, size and clear. Requests, typed results/errors, result class and timestamps are stored as JSON strings. Receivers, object arguments and object results are retained while the log owns them. The log itself can be inspected.

POST /recording accepts action start|read and objectId for read. Its response contains objectId, records, script, test and error. Script/Test choose an editable generated source buffer. Download saves it as recording.smt. Load into REPL copies it into the main editor for execution. Export recreates objects created by recorded expressions and maps their IDs to replay result variables for subsequent calls and arguments. Existing external objects remain explicit #@id references. Per-receiver ObjectWorkspace instances preserve scratch bindings when replaying workspace entries. Tests assert primitive values and object result classes and verify recorded errors are signaled. Script exports retain ordinary failure behavior. Recording does not capture automatic inspection reads or the export operation itself.

## Native method debugging experiment

The inspector's Debug method button opens a debugger for its receiver. The header Debugger button reopens the most recently selected receiver. Choose a native method, enter typed arguments, then Start paused. The request queues a debug execution on the receiver's normal mailbox. The evaluator holds that method's context and stops before its first statement. Other normal messages queue behind it; pulse, inspection and debugger commands use the control lane. Closing the debugger leaves the session available for reopening.

The frame displays the running definition's statements, current statement, typed arguments, receiver fields and last result. Step executes the current statement then stops before the next. Continue runs the remaining statements until return or an uncaught signal. These operations retain the original evaluator context and never repeat preceding statements. Arguments and receiver fields may be edited at a pause. Optional Smalltalk replacement code executes in place of the current statement for this invocation. It does not change the class definition. Calls within one statement execute normally; this experiment steps the selected native method's outer statements, including methods inherited from native superclasses. Foreign/built-in methods without a native method body are rejected.

An uncaught signal stops at its statement and displays the signal. Retry statement repeats that statement, using replacement code and edited values if supplied; any earlier side effects, including partial effects within the failed statement, remain. Return value completes the method with the selected typed value. Abort releases the frame without executing further statements. A pause expires after ten minutes. Commands carry a session ID and pause revision; stale or duplicate commands fail without resuming execution. Invalid replacement syntax, unknown fields/arguments and malformed Values are rejected while the frame remains paused. Completed results and argument object references remain reachable through the receiver's debugger state until the next session replaces it.

DebugRequest contains objectId, method and typed args. DebugCommand contains id, revision, action (step, continue, retry, return, abort), optional replacement code, arguments and fields maps, and a typed return value. DebugSnapshot contains id, objectId, method, state (queued, paused, running, completed, aborted, expired), statement index, revision, arguments, statement text, result, signal/error and pause deadline. RuntimeInspection includes the current optional debug snapshot. Native object messages __debugStart: and __debugCommand: accept JSON strings; __debugJson returns a JSON snapshot. POST /debugger accepts action start/read/command, the DebugRequest fields and optional command, and returns {snapshot,error?}. Automatic inspector property and view reads are skipped while the receiver is paused or running a debug session.

## Message tracing

The header Message trace button opens a trace browser. Start tracing clears old records and enables collection on the coordinator and all currently registered objects. Objects registering afterward join the active trace before becoming available to spawn callers. Stop tracing stops collecting new messages and keeps existing records; replies to already recorded calls still complete their entries. Refresh reads current events. Clear removes recorded events. Filter by method, object ID or signal text; Follow chain selects all recorded messages sharing the call-chain ID, and All chains removes that filter. Receiver/sender/result object links open inspectors. Download JSON exports the current fetched trace. Cards wrap within the viewport.

MessageRequest adds optional sender. Outbound object sends populate the sender ID; external callers may leave it empty. Each receiving object keeps up to 1,000 TraceEvent records, dropping the oldest and counting discarded records. Events have id, chainId, sender, receiver, method, typed args, started/finished timestamps, durationMs, state (running, returned, signaled), typed result and optional error. Control/inspection/pulse traffic is excluded. Normal native and foreign dispatch share this instrumentation. Clearing invalidates outstanding completions so old calls cannot recreate cleared entries. Historical object IDs in traces do not become GC roots.

GET /trace returns TraceSnapshot {enabled,events,dropped,errors?}, sorted by start time; optional objectId and chainId filter results. POST /trace accepts TraceRequest {action:start|stop|clear}. Partial object failures are reported by object ID while other results remain available. Ordinary object messages __trace: (start/stop/clear) and __traceJson expose the same per-object records through MCP. The coordinator's trace setting applies to the runtime session.

## Source editing, Git history and new definitions

Selecting a method in Browse methods offers Edit source / Git history. The source panel shows the entire class definition in an editable text area and its latest thirty file commits with revision, subject and local date. Selecting a commit displays that historical file. Copy revision to editor puts its text into the draft; Save definition is the explicit write operation. Working diff displays the saved file's difference from Git's index. Refresh history updates revisions while preserving the draft. Close preserves per-class browser drafts. Git object opens the ordinary Git object's inspector.

Save definition validates exactly one native class, its unchanged name, syntax and superclass chain. It rejects extra executable forms, unavailable parents and inheritance cycles. The request carries the digest of the file that was opened; stale edits fail and preserve the draft. The server writes a temporary file beside the definition, preserves permissions, syncs and atomically replaces the existing definition. Source writes handled by one coordinator are serialized. Embedded definitions and foreign manifests are readable in this panel; the native class editor marks them read-only. Only classes discovered through the existing source browser can be addressed. Filesystem symlinks resolve to the actual source file.

Saved definitions apply when new object processes load the class. Existing objects continue running their loaded definitions and retain their state. Local subprocess tests verify old and new instances coexisting with different behavior. The existing worker spawn-source-bundle mechanism includes the current source for subsequent worker objects. This workflow does not replace code inside running foreign processes.

Commit this file requires a commit message and a saved editor buffer. It verifies the displayed file digest and calls the Git object's commitFile:message:. That operation stages the specified file and commits only that path, preserving other staged files. The panel refreshes history after a successful commit; errors remain visible. Source history operations invoke the ordinary Git class through typed messages and explicit argument arrays. Each source directory has a rooted Git object so its configuration can be inspected and adjusted. The editor does not push automatically.

Git adds history: (latest thirty file commits, full revision/unix time/subject separated by NULs), version:file: (file content at revision), diff: (working diff for one path), and commitFile:message:. Existing authentication settings and explicit argv execution apply to these methods. Source revision reads accept full hexadecimal commit IDs and use paths relative to the Git object's source directory.

POST /source accepts SourceRequest {action:read|save|revision|diff|commit,class,source?,digest?,revision?,message?}. SourceResponse contains class, canonical file, current source and digest, readOnly, revisions ({id,timestamp,subject}), historicalSource, diff, gitObject, optional gitError and operation error. A source file outside a Git repository can still be read and edited; Git errors are reported separately from successful file operations. Native source serialization and the on-disk class formats remain unchanged.

## Native clones and prototype derivation

The inspector offers Clone and, for native definitions, Clone and edit. Clone sends the ordinary clone message, retains the returned result through the interaction system and opens the new object. Native fallback cloning creates a separate process with independent field, Array and Dictionary storage. Object-valued fields/elements and block lexical environments remain shared references, matching shallow-copy semantics. Native HttpClient and workspace instances can also be copied. A native or foreign class's own clone method takes precedence, allowing objects with external resources to implement their own copy policy. Process, HttpServer, RobotsMatcher, SitemapParser and foreign-state objects without a custom clone method signal an explanatory error.

Clones carry the already-loaded method hierarchy, including superclasses and super-send behavior. They do not reread their class files. Initialization is not repeated. Saving new source can therefore coexist with both existing objects and clones of those objects. Runtime inspection adds prototype (the original object ID) and definitionVersion (a SHA-256 digest of the loaded native definition hierarchy). The object graph labels a separate cloned-from edge. This historical origin is not a GC ownership reference.

Clone and edit opens typed field inputs plus an optional method selector, comma-separated parameter names and Smalltalk body. Creating the derived object sends cloneWith: with a JSON PrototypePatch {fields,methods:[{name,params,source}]}. Declared fields, parameter names, selector arity and method syntax are checked. The patch changes the new object's loaded methods and fields only. Derivations can themselves be cloned or used as the origin of another derivation. Runtime-reserved method selectors are not replaceable through a native method body. No class-source file is required for a derived method. The system still reports the original class name alongside its distinct definitionVersion.

NativeDefinition contains name, vars, methods (existing MethodDef/Node syntax records) and optional superclass. NativeSnapshot contains definition, fields, array, dictionary, block and prototype. SpawnRequest adds optional snapshot; the coordinator validates a matching class, disables autoInit and rejects simultaneous blockDef. Both local and worker spawns serialize a temporary snapshot file and pass --snapshot to the child. The child validates/restores all native state before subscribing/registering. Snapshot files are removed after use and on spawn completion. Native snapshot syntax is depth-bounded and Value-validated. __nativeSnapshotJson exposes a snapshot through the control lane for inspection and derivation UI.

## Visual message wiring

The header Wiring button opens the rooted main Wiring object. Wiring is an ordinary loadable class with definition (JSON), retained (Array) and lastRun (JSON) fields. Its public methods are definition/definition:, retained, lastRun, run and resume, plus inspector metadata/views. Definition changes validate the whole graph before storing it and retain its literal object references. Each new run resets the result references, retains graph inputs, and keeps every object-valued result. Closing the editor does not destroy the graph object. Inspect graph object opens its ordinary inspector; the inspector's Open graph button opens the same graph editor.

Choose a live receiver and Add node to add a method box. Selecting a box opens its receiver, selector and typed argument editors. Method selection supplies known parameter names and argument count. Apply node updates the draft. An input may be a literal Value or a prior node's result. Click a filled output socket, then a receiver or argument socket to connect them. Drag boxes to arrange them; Fit graph fits their positions. Remove node removes it and turns its former connections into nil literals for editing. Save graph sends definition: to the Wiring object. Run graph saves pending changes and invokes run. Successful nodes show their typed results; object results can be inspected. Error nodes are marked and show the signal. Resume from failure explicitly retries the failed node and continues, retaining prior successful results. Closing/reopening rereads the saved graph. Graph and detail panes stack on narrow windows.

WireDefinition contains nodes. Each WireNode contains id, optional label, target (WireInput), method, args (WireInput array), and optional x/y positions. WireInput contains exactly one of value (a typed Value) or from (another node ID). The engine rejects duplicate/missing IDs, invalid Values, missing connection targets, cycles and graphs over 128 nodes before dispatch. It derives deterministic dependency order from node order and calls the evaluator's typed message dispatcher. Primitive receivers dispatch normally; native and foreign objects receive ordinary messages. Connections can carry object identity as well as primitive data.

WireRun contains definitionVersion (digest of graph JSON), results by node ID, completed order, state (running/completed/failed), failedNode and error. The Wiring object's lastRun is updated after each completed node and before propagating a failure as a normal runtime signal. resume requires the same definition digest and a previous failed/completed run, skips completed nodes, and never implicitly retries successful operations. Retrying the failed node can repeat partial effects within that node. A completed run resumes without re-execution. run starts a fresh execution. The primitives validateWiring:, retainWiring:in:, executeWiring:retain: and executeWiring:retain:previous: provide parsing, reference retention and typed execution to the ordinary class. POST interaction recording captures explicit graph saves/runs like other object messages.

GET /wiring, optionally with id, returns WiringResponse {objectId,definition,run,error?}. Omitting id creates/reuses the rooted main Wiring object. The UI edits its definition and invokes its methods through the existing typed interaction interface. The native/foreign and signal/resume contracts are verified with real object subprocesses.

## Mobility investigation

Native snapshots demonstrate that loaded behavior and runtime-visible state can be copied onto a worker while preserving super sends and object references. Current clone spawning allocates a new object identity. Moving an existing object would additionally require quiescing its mailbox, handing off its NATS subject/registry identity without two active receivers, accounting for in-flight calls, transferring GC-root/placement ownership and restoring language-specific external state. Foreign bridges, open processes/sockets and active evaluator stacks do not expose a portable transfer protocol. A future native-only handoff could use an explicit quiescent snapshot protocol; cross-language or cross-OS objects need per-runtime export/import adapters. No arbitrary object migration is implemented or promised. This item remains research rather than a requested migration feature.

## Inspectable asynchronous messages

Promise is an ordinary native class with fields state, target, selector, arguments, result, error, startedAt, finishedAt and timeoutMs. initialize sets idle state. send:selector:arguments: sends an object message asynchronously with a 30-second reply deadline; send:selector:arguments:timeoutMs: sets an explicit positive deadline. The send returns the Promise immediately. Arguments are copied into a retained Array before dispatch, and the receiver and eventual object result remain reachable through the Promise. Native, foreign and built-in receiver objects use the same transport. A Promise is single-use: another send signals an error instead of repeating work. Its public state, target, selector, arguments, error and ready getters describe progress; result returns a fulfilled typed Value or signals pending/rejection. await waits up to 30 seconds; await: sets the waiting deadline. A waiting timeout leaves the operation available for later inspection or another await. A transport deadline rejects the Promise and reports that the receiver may still be running; it does not claim to cancel remote work.

The runtime primitive startPromise:method:arguments:timeoutMs: validates the receiver, argument Array and timeout, snapshots the arguments, records pending state, then sends on a separate call chain. Completion atomically stores the typed result or error and signals waiting callers. promiseResult and awaitPromise: implement result retrieval and waiting. Rejected signals remain normal runtime signals through MCP and tests. A pending Promise cannot be cloned; settled Promises can be cloned with their completed fields and shared result reference. The active request appears among ordinary inspector waits and tracing chains. The control-lane inspection remains responsive during normal-mailbox await calls.

RuntimeInspection adds an optional PromiseSnapshot containing state, target Value, selector as method, argument Array Value, result Value, error, ISO timestamps and timeoutMs. The inspector renders this snapshot directly with links to receiver, arguments and object result, so inspecting does not wait behind the target's call. Pending inspectors refresh every 500 milliseconds until they settle or are closed. Use result sends result and places its typed value in the reusable result shelf.

The inspector message form adds Send asynchronously alongside Send. It uses the current selector and typed arguments, returns a retained Promise and opens that object's inspector. The graph editor adds Run asynchronously, which saves any changed graph, starts its run through a Promise and opens that Promise. Reopening the graph reads the latest saved run. Browser asynchronous actions use a ten-minute reply deadline. InteractionRequest accepts kind promise and optional timeoutMs; omitted/zero timeout selects the browser default. Interaction recording exports an equivalent Promise send with the same receiver, arguments and deadline. The browser runtime uses no new dependency.

## Live workbench and runnable documentation

The main toolbar is rendered from a rooted Workbench object. Its definition is JSON with title and tools. A WorkbenchTool has id, label and action; inspect/call tools additionally carry objectId, and call tools carry method and typed args. Supported actions open results, recording controls, method navigation, object graphs, tracing, debugging, wiring and examples, inspect a named object, or send a typed message. Buttons preserve definition order. Definitions require a title, unique identifiers, nonempty labels, recognized actions and no more than forty tools. Results and Record may appear at most once. The object validates definition: before replacing its current definition and retains every referenced target and argument in its retained Array. Browser code handles rendering and input events; runtime objects own tool definitions, example workspaces, inspector views, recordings and message graphs.

A fixed Workbench button opens the definition editor even when the toolbar definition has removed other tools. The editor opens with visual controls. Save tools validates and stores the edited definition through an ordinary message, then reloads the toolbar. Reload reads the saved definition. Restore default tools calls resetTools. Inspect workbench object opens its normal inspector; closing that inspector refreshes the toolbar. Native source editing and Git history can change the Workbench class's defaults for later instances. Current live definitions stay with their objects.

Workbench owns an examples Array, populated lazily with seven RunnableExample objects. Each example has title, description, source, check, workspace and lastRun fields. It allocates its own workspace through the coordinator. run evaluates source with typed result and captured transcript, then evaluates check while preserving the source result. An uncaught signal in either stage propagates as a failed interaction. result returns the workspace's last value. reset replaces the workspace and clears lastRun. Individual text getters/setters and updateTitle:description:source:check: make the documentation editable with ordinary messages. Workbench addExampleTitle:description:source:check: adds and returns a new example. Defaults cover counters, clones, prototype derivation, message wiring, promises, signal handling and explicit-argument process execution of the Go tool.

Examples opens a catalog alongside editable title, description, source and check fields. Selecting an example restores its browser draft; switching or closing keeps unsaved drafts in the page. Save example updates the live object. Run and check saves changes, executes in that example's workspace, shows Passed or Failed and renders the typed reusable result and transcript. Object results open inspectors. Inspect example and Inspect workspace expose the backing objects. Reset workspace starts a fresh scratch environment for that example. New example adds an editable example initially returning and checking 42. The layout stacks at narrow widths. Main workspace bindings are unaffected by examples unless their code explicitly accesses shared objects.

GET /workbench returns WorkbenchResponse {objectId,definition,error?}. GET /examples returns ExamplesResponse {workbench,examples,error?}; each ExampleDescription has objectId, title, description, source, check and workspace. Editing and running use the typed interaction interface. InteractionRequest kind example calls run, decodes its WorkspaceResult, retains the result and records the interaction. Recording export invokes the example then reads result. workbenchReferences: validates a definition and returns its retained object references. No executable browser code is accepted in a workbench definition.

## Parsed selector refactoring

Selecting a native method in Browse methods offers Rename selector. The panel supplies the original selector, an editable replacement and a scope selector for the current class or all native classes. Preview changes parses the source files and lists matching definitions and explicit sends by class and line, with expandable proposed source. Unary, binary and keyword replacements must keep the original syntax category and argument count. A class that already defines the replacement selector rejects a conflicting rename. Foreign and embedded definitions are excluded from writes.

The Smalltalk parser records exact source spans for method selectors and explicit sends, including nested expressions, super sends and cascade tails. Constructor syntax, Transcript output and JSON syntax folded into runtime forms are not renamed as ordinary method calls. The S-expression parser records spans for method headers, sends and cascade selectors while excluding quoted forms, field declarations and parameter lists. Replacements operate only on the recorded spans, preserving comments, whitespace, bindings and other source text. Keyword parts are changed independently around their existing arguments and comments. Source offsets count Unicode code points. Exact string/symbol references, including Smalltalk literal-array entries, are listed by line for manual review and are left unchanged; runtime-computed selector values are outside this source transform.

Preview never writes. Each Save CLASS button writes that selected proposed definition through the existing source API with the preview's original digest. The full proposed class is validated before preview and again before save. A stale digest rejects the save, and the panel shows the error. Successful saves display Saved and disable that button. Source / Git history opens the existing editor/history workflow for the selected class. Saving applies to newly spawned objects; existing processes keep their loaded definitions. Class scope changes only that file; all-native scope presents independent per-file saves so the user can review affected senders and implementors.

POST /refactor accepts RefactorRequest {from,to,class?} and returns RefactorResponse {files,errors}. Each RefactorFile contains class, canonical file, original digest, proposed source, edits and literal-reference line numbers. RefactorEdit contains line, kind (definition or send), before and after. Parser metadata uses SelectorSite {selector,kind,spans} and SelectorSpan {start,end,line}; spans are half-open code-point ranges. This metadata is collected only for source analysis and does not alter the serialized evaluator AST.


## Visual view and toolbar editors

The fixed Workbench button opens a visual tool editor. Workbench title changes the header. The Tools list selects a tool; Tool label, Tool identifier and Tool action edit its definition. Add tool creates a unique tool identifier, with Browse methods as its initial action. Move tool up/down changes toolbar order; Remove tool removes the selected definition. The toolbar preview selects tools without executing their actions. Inspect an object and Send a message expose Target object, with suggestions from the live registry. Send a message additionally exposes Tool method with alphabetically sorted live method suggestions, typed arguments, Add argument, Remove argument and Match method arguments. Match uses declared parameter names and arity, falling back to selector syntax. Argument editors accept numbers, strings, booleans, nil, object references, characters and retained results. Retained results resolve to typed literal values when selected. Invalid arguments block saving and switching selection until corrected. Save tools sends definition: to the Workbench; server validation errors remain visible and leave the saved definition unchanged. Reload reads the saved definition; Restore default tools resets the object. Close and Inspect workbench object preserve the browser draft. Edit JSON and Visual editor switch modes, preserving extra definition properties; malformed JSON remains editable with its error displayed.

Edit layout in an object view opens a View designer over the inspector. Layout lists controls in tree order, indented by nesting; selecting a tree entry or clicking its preview selects that control. View title changes the selected view's tab title. Control kind chooses column, row, text, value, code, input, slider, textarea, action, table or list. Control label sets the caption. Text/code/value controls expose Literal text and Getter method; actions expose Button text, Action method, typed arguments and Refresh after action. Input/slider/textarea expose Getter method, Setter method and Value type, which can be inferred from the getter. Sliders add Minimum, Maximum and Step; tables add editable column keys/labels, Add column, Remove column and Infer columns from data. Non-container controls expose typed getter/action arguments. Method fields suggest live selectors. Definitions retain unknown properties when edited visually or through JSON.

Choose New control kind and Add control to add inside a selected row/column, after a selected leaf, or wrap a leaf root in a column. Move up/down reorders siblings. Nest in previous moves a control into the immediately preceding row/column; Move out lifts it after its parent. Duplicate copies a subtree. Remove control removes the selected subtree. The root cannot be removed, duplicated or moved. Preview uses the regular view renderer and reads live getters. Clicking or pressing Enter/Space on preview controls selects them and does not send their actions or setters. Refresh preview rereads values. Save layout validates structure, nesting (maximum sixteen levels), slider bounds and action selectors before sending definition: to the InspectorView and refreshing the receiver. Preview errors appear in the corresponding control. Edit JSON and Visual editor allow both representations; invalid JSON remains visible until corrected. Close preserves per-view drafts, including unparsed JSON, in the browser tab; Reload saved layout discards them. Browser reload clears unsaved drafts. Saved changes belong to the live InspectorView object; class defaults are unchanged. The editor stacks its panes on narrow screens.

The visual-editor.js route serves shared field/argument helpers, workbench editing and view design. View rendering accepts an optional decorator with the element, node and index path so the designer can add selection behavior while sharing the production control renderer. Browser editor state contains the selected owner/receiver, a copied definition, selection path/index, current JSON mode, dirty flag and per-object drafts.


## Live GUI controls

InspectorView root is a live control object. Existing JSON layouts are converted when first assigned/read. Each node becomes a separately inspectable native object: Button for action, Label for text/value/code, Input for input/slider/textarea, Row/Column for layouts, and Control for table/list. These classes derive from Control and use normal source loading, method dispatch, object identity, placement and GC. The ordinary object graph displays their references. Browser rendering remains the platform boundary.

Control fields are kind, label, text, receiver, getter, setter, selector, arguments, children, owner, wiring, refresh, valueType and options. Receiver, owner and wiring are typed references or nil. Arguments and children are Arrays retaining their elements. Options is JSON for additional rendering properties such as slider limits, table columns and extension fields. Public getters/setters expose label, text, receiver, getter, setter, method (stored as selector), arguments, wiring, refresh, valueType and options. kind and owner are readable. children returns the child Array; children:, add: and remove: validate ownership and structure. A child has one owner, either a layout or InspectorView; remove it before attaching elsewhere. These operations reject cycles, duplicate children, non-control children and children on leaf controls. Trees are bounded to 128 controls and sixteen nesting levels. Argument Arrays are bounded to 128 values. External direct Array/raw-field edits are still possible; tree serialization detects cycles/shared children instead of recursing indefinitely.

Control effectiveReceiver uses its receiver when supplied, otherwise recursively asks its owner. InspectorView supplies its target. The view method follows owners to the InspectorView or returns nil for unattached controls. read returns literal text when getter is empty, otherwise sends getter with arguments to the effective receiver. write: sends setter with the supplied typed value and signals when no setter is configured. invoke sends method with arguments, or sends run to the assigned Wiring object. Missing action selectors and receiver signals propagate normally. These messages work with native and foreign receivers. They are available through MCP and normal Smalltalk expressions.

InspectorView root: attaches an unowned control tree, retaining it and setting its owner. definition returns the current live tree, including each controlId; it reflects changes made through MCP, inspectors or other objects. definition: validates the entire JSON tree and control IDs before mutation, then creates/configures controls and stores the root. Existing IDs belonging to the same view are preserved across edits and reordering while their control class stays compatible; changing to another control class creates a new identity. New nodes omit controlId. Duplicate in the designer strips IDs from the copied subtree. Foreign/stale IDs and duplicate IDs reject the edit. Removed subtrees are detached at their roots, preserve their internal ownership, and exclude children moved into the saved layout; other references can keep them alive. JSON remains an import/export representation and carries unknown rendering fields, but runtime fields and references are authoritative for known properties. The root/root: and definition/definition: interfaces are usable without the browser.

The designer shows the selected control's ID and Inspect control object. The resulting ordinary inspector offers Open control in designer to return to its containing view and select it. New controls acquire IDs after Save layout. Control receiver accepts a live object ID with registry suggestions; empty means inherit. Method suggestions use the selected control's effective draft receiver. Action source chooses Send a message or Run Wiring graph. The latter offers Wiring object suggestions and Open Wiring graph; the live button invokes that graph's run method. Each input sends write: to its control and each button sends invoke, preserving interaction results and recording behavior. The preview reads draft bindings, while preview click/Enter/Space only selects controls.

An open view checks its live layout once per second. Layout changes made through ordinary messages appear without browser reload. Refreshes leave focused inputs and unsaved designer drafts intact; Reload saved layout explicitly replaces a draft. Unchanged layouts do not rerender or invoke getters during polling. Closing/changing inspectors stops the old polling loop. Control state and Array snapshots use the existing inspection channel, avoiding a normal-mailbox cycle between a view exporting its layout and a control resolving its owner/receiver. Public control setters, tree operations and action methods remain normal serialized calls; the internal ownership/configuration primitives use locked field replacement on the inspection channel.

ControlProperties is the typed wire subset (kind, controlId, label, text, getter, setter, method, type, receiver, wiring, refresh, args). ControlState contains its serialized property map and typed child references. ControlPlan holds parsed properties, extra raw JSON fields, child plans and allocated identity during reconciliation. The runtime primitives __replaceView:, __describeView, __viewRoot, __viewRoot:, __controlState, __controlOwner, __controlOwner:, __describeControl, __controlConfigure:owner:children:, __controlChildren:, __controlAdd: and __controlRemove: bridge ordinary objects to JSON serialization and validated tree operations. __controlState runs on the inspection channel and takes coherent field/Array snapshots; it does not execute application getters. Ownership access and validated control configuration also use that channel, so a layout save can finish while a control awaits its inherited receiver. Field snapshots and replacements are locked.


### Workbench presentation and object sections

The workbench uses a system sans-serif UI font, monospaced code and IDs, warm neutral surfaces, restrained terracotta emphasis and visible keyboard focus. A header contains the product name, the configured workbench title (Workbench for the default title), and a connection indicator updated by registry requests. The desktop left sidebar contains configured tools in their saved order and Customize tools, which opens the existing workbench editor. On narrow screens the tools form a horizontally scrollable strip. Existing tool actions and definitions are unchanged.

The main Playground area labels Expression and Output separately. Run expression evaluates the editor; Ctrl+Enter remains available. Clear output clears the output, whose empty state reads Results appear here. Below, Live objects has a case-insensitive class/ID search, visible/total count, and Runtime objects checkbox. The checkbox starts off and hides builtin classes whose names start with two underscores. Enabling it includes those objects. Rows sort by class then ID; row click and the keyboard-accessible Open button inspect the object. Periodic refresh preserves focus on the same object's Open button. Empty filters report No matching objects.

The object dialog has a fixed header with class, subdued ID, Close and four sections: Interface, Methods, State and Workspace. Interface is the initial section when the object provides a view. It displays that view's controls and title, selected view tabs when there are multiple views, Inspect view object and Edit layout. State, runtime metadata and methods are not displayed in Interface. Methods is the default for objects without views; it contains a selector filter, alphabetically ordered buttons, the existing message form and an expandable Object tools group containing browsing, graphs, debugging and cloning. Clicking unary selectors still invokes them immediately. State contains editable properties, fields and collapsed Runtime details, including placement, retention and calls; Promise details remain accessible there. Workspace contains the existing object expression workspace. Invocation/property results appear below Methods or State. Refreshing an object after an action preserves the active section. Arrow keys, Home and End move between section tabs; Tab stays within the dialog. Close and Escape dismiss it and return focus to the opener when it is still present.

The view designer retains its existing actions and model, with the layout outline at left, the interactive preview in the centre and selected-control properties at right. On narrow screens preview, outline and properties stack. All tools use the same typography, borders, spacing and controls; tables scroll within their panes and narrow screens do not overflow horizontally. Interface inputs, live control identities, receiver bindings, Wiring actions, recording and saved definitions retain their existing semantics.

When a coordinator reloads a registry, its next REPL request adopts the sole surviving pinned builtin workspace instead of spawning an empty replacement. Unpinned example workspaces and non-workspace roots are excluded. Multiple pinned workspace candidates produce an explicit ambiguity error naming the roots. This permits updating the coordinator while surviving object processes keep their identities and state.


## Assisted discovery and examples from interactions

The Playground output starts with Explore ServiceTalk actions: Try an example opens Examples, Find an object focuses live-object search, and Find a method opens method navigation. Clearing results restores these starting actions. Every object inspector offers Find examples, which searches example titles, descriptions and source for the receiver class and selects a matching example. Declared metadata properties add Read property shortcuts that explicitly send the getter and show the result in State. The Methods message form offers Find callers and implementations for its selected selector; a known selector also offers Debug this method with that selector selected. An unknown selector shows up to three similar known selectors; choosing one fills the form without sending it. Examples search filters titles, descriptions and source, displays No matching examples when empty and restores the editor when matches return.

Successful Playground evaluations, inspector calls, object workspace expressions and object-view actions offer Save as example. It creates a RunnableExample with editable source and no check, displays its observed typed result separately, and opens it in Examples. Primitive observed results offer Use observed result as check; this fills an editable equality check which is saved by Save example or Run and check. Object results remain inspectable and do not automatically assert object identity. Running with an empty check reports Completed (no check); a failed accepted check reports Failed. Control actions appear in a persistent result area below their view so refresh does not remove the action. Example names from controls use their visible labels.

Before a Playground or object workspace expression runs, the browser samples existing bindings whose names occur in the expression, including it; it stores their typed values with the interaction result. Save as example captures these bindings in the example. A Playground example executes the original source in its own workspace. An object-workspace example builds a fresh ObjectWorkspace for its original receiver, restores sampled scratch bindings and evaluates the original expression there. A captured method call retains the receiver and object arguments and generates the corresponding unary, binary or keyword send. Primitive arguments are rendered as literals. Captured objects are references to the same live objects; running an example performs its messages again. Explicit object IDs written directly in an expression are not automatically retained unless also held by a captured binding. A failure to sample inputs is reported when attempting to save the example, without preventing the original interaction.

RunnableExample stores captured name/value pairs in its own retained Array. capture:value: also installs the binding in its current workspace; each run restores captured inputs, and reset creates a new workspace and restores captured pairs, including observed result/check metadata and referenced objects. Existing examples retain their prior behavior. All resulting examples remain ordinary editable objects.
