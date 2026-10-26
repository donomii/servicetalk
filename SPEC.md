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
