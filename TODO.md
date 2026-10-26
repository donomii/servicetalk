# TODO

## Next up (agreed priorities)

Picked as the next batch of work; detail lives in the sections noted.

- **Worker re-registration.**  ✅ Shipped.  Heartbeats are self-describing —
  every beat carries the worker's host/port, so the coordinator upserts the
  worker from any beat.  A coordinator restart needs no handshake: the next
  beat repopulates the worker table.  No separate `/worker-register` endpoint
  or re-register branch.  See `worker.go` sendHeartbeat and `coordinator.go`
  handleWorkerHeartbeat.
- **More evaluator test coverage.**  ✅ Shipped (this pass).  Added
  integration cases for `doesNotUnderstand:` forwarding (new `Proxy.st`
  fixture), cascade return value, and `perform:` / `perform:with:` /
  `perform:withArguments:` across number/string/dictionary receivers.
  Three-level inheritance is now committed and deterministic: `Tiger.st`
  extends `Cat`, which extends `Animal`, and the in-process suite covers
  inherited fields and methods, two-hop `super`, missing transitive parents,
  and three-class cycles.
- **`smt build` / `smt run`.**  ✅ Shipped.  `smt run DIR` runs an app
  directory (classpath + `--main Main`, then serve).  `smt build DIR -o BIN`
  bakes the app's `.smt`/`.st` classes into a single self-contained binary
  via `go:embed` (needs the Go toolchain + source; pure-ServiceTalk only —
  foreign classes still go through `bundle.sh`).  The built binary is
  turn-key: run with no subcommand and it spawns `--main` and serves.  See
  README "Packaging for deployment" and `build.go` / `embed.go`.  Multi-target
  cross-compiled archives remain `bundle.sh`'s job.
- **`Character` type — must be fully Unicode-aware.**  ✅ Shipped.  A
  `Character` primitive holds one rune; literal `$x` (incl. `$ ` and non-BMP
  like `$😀`); constructed via `n asCharacter` / `aString asCharacter` (no
  `Character value:` — there are no class objects).  Methods: `asInteger` /
  `value`, `asString`, `printString` (`$a`), `asUppercase` / `asLowercase`,
  `isLetter` / `isDigit` / `isUppercase` / `isLowercase` / `isSpace` /
  `isPunctuation`, and `= ~= < > <= >=` by code point — all via Go's
  `unicode` package, correct across the full range.  Decision: `String at:`
  / `asArray` / `chars` still return one-rune **strings** (backward compat);
  `asCharacter` bridges.  See README "Characters" and `character.go`.

## Pulse-based GC — followups

Shipped.  The coordinator sends `pulse:` against its root set
(workspace + the `--main` class) every `SMT_PULSE_INTERVAL_MS` (default
10 s) using `time.UnixMilli` as the sweep id; objects forward to every
objid in their `instanceVars` / `arrayData` / `dictionaryData` /
captured block args; `pulse:` is overridable per class.  Idle timeout
is gone.

Still outstanding:

- **Pinning API.**  ✅ Shipped.  `coordinator pin: anId` / `unpin: anId`
  / `isPinned: anId` add/remove an object from the pulse-root set; see
  README "Coordinator as an object".
- **CLI flags for the timing knobs.**  ✅ Shipped.  `coordinator`
  accepts `--pulse-interval`, and object processes accept
  `--pulse-timeout`; the env vars still work for packaged deploys.
- **Foreign-class reference tracking.**  ✅ Lint shipped.  `BuildForeignClass`
  warns when a foreign class calls `st_send` (C) / `send` (Rust) but
  declares no `pulse:`.  The deeper fix (the runtime actually seeing
  C/Rust-side refs) is still not possible — the warning + a hand-written
  `pulse:` remain the mechanism.

### Registry persistence

✅ Shipped.  `coordinator --registry-file PATH` (or `SMT_REGISTRY_FILE`)
snapshots the registry + roots to disk and reloads/revalidates on
restart.  Still in-memory by default; not an HA story (coordinator
downtime still fails registry lookups).  See README "Registry
persistence".

## NATS transport

✅ First pass shipped.  Object processes no longer own HTTP listeners or
TCP ports.  Each object subscribes to `smt.<cluster>.obj.<object-id>.call`,
and all method calls use NATS Core request/reply.  Registry entries carry
`{object id, class, subject}` instead of `{host, port}`.  Object-side
dispatch is FIFO with a separate control lane, same-chain reentrant calls,
mailbox-full rejection, and startup retries the initial NATS connection
before failing.

For self-contained local runs and test launchers, `SMT_EMBED_NATS=1` starts a
loopback-only broker inside the coordinator and supplies its generated URL to
child objects.  The default remains external `nats://127.0.0.1:4222`; the
embedded broker stops when a one-shot command exits.

One-shot and HTTP evaluations default to a 30,000 millisecond workspace
deadline.  `SMT_EVAL_TIMEOUT_MS` raises or lowers that deadline for suites or
interactive evaluations that intentionally run longer.

`HttpServer listenOn: 0 handler:` asks the operating system for an available
port; `server port` returns the assigned port.  Fixed ports remain the default
for applications, while local suites can avoid collisions without probing.

Shipped followups:

- **Coordinator + worker smoke test.**  `TestIntegrationCoordinatorWorkerSpawn`
  starts embedded NATS, a coordinator process, and a worker process; waits for
  worker registration; then evaluates code through remote worker spawn.
- **Worker placement.**  The coordinator now reserves workers by lowest
  in-flight spawn count, then lowest assignment count, then worker id.  This
  spreads sequential spawns and avoids piling concurrent bursts onto one
  worker.
- **Mailbox-full semantics.**  Normal and control mailboxes return an
  immediate remote signal when full.  No automatic retry; application code owns
  retry/backoff if it wants it.
- **Source-bundle guardrails.**  Worker source bundles reject invalid paths,
  digest mismatches, more than `SMT_SOURCE_BUNDLE_MAX_FILES` files, or more
  than `SMT_SOURCE_BUNDLE_MAX_BYTES` total bytes.
- **Request-size guardrails.**  Coordinator/worker internal HTTP endpoints
  enforce `SMT_INTERNAL_HTTP_MAX_BYTES`; outbound NATS method calls enforce
  `SMT_NATS_MAX_CALL_BYTES` before contacting the broker.

Shipped followups:

- **Benchmarks.**  Harness covers small calls, fan-out, pulse traffic,
  p50/p99 latency samples, message rate, queue depths, CPU count, fd count
  where the OS exposes it, large payloads, and broker restart recovery.
- **Transport interface.**  Object call routing now goes through a small
  `objectTransport` interface; NATS Core remains the only implementation.

## Program packaging / distribution

`smt build` parses every `.smt` and `.st` source before staging it, requires
exactly one class per file, and requires the declared class name to match the
filename.  Invalid app sources now fail the build instead of producing a
binary that fails only when the class is first loaded.

A servicetalk "program" is currently "the `.smt` files in the current
working directory plus whatever Main class you pick".  That's not a
distributable unit — the binary has to live next to the classes, and the
user has no way to say "this directory is my app."

Steps, from least to most invasive:

1. **Class search path.**  ✅ Shipped.  `--path DIR` / `SMT_PATH=dir1:dir2`
   plus binary-relative fallbacks (see `SetClassPath`).  `smt run ./myapp`
   wraps the common `coordinator --path ./myapp --main Main` shape.
2. **Bundled distribution.**  ✅ Single-binary shape shipped: `smt build
   ./myapp -o ./myapp` bakes `.smt`/`.st` classes in via `go:embed`
   (`build.go` / `embed.go`).  Pure-ServiceTalk only.  The archive shape
   (`smt bundle → myapp.smtpkg` + `smt run myapp.smtpkg`) is **not** built —
   `bundle.sh` already covers cross-compiled multi-target archives, so the
   `.smtpkg` form was skipped as redundant unless a cluster workflow wants it.
3. **Cluster deployment.**  ✅ Spawn-time source bundles shipped.  When
   the coordinator delegates to a worker, it includes a digest-keyed
   bundle of the current classpath's `.smt`, `.st`, and foreign-class
   sources.  The worker caches it and puts that directory first on the
   spawned object's classpath.

Note: foreign-class directories follow the per-class-directory convention, so
the disk classpath lookup applies to them with no extra work — but `smt build`
single binaries can't embed them (they need on-disk compilation); use
`bundle.sh` for apps with foreign classes.

## Foreign classes — followups

Shipped in 862ceec5.  See README "Foreign classes (C)" for the user-facing
docs and `CCounter/` for the worked example.  Diverged from the earlier
sketch in two places worth remembering:

- **Numbers and booleans are primitives at the C boundary**, not objects.
  The "everything is a message" purity was impractical for hot-path code
  (one round trip per `+`).  `st_isNumber` / `st_asNumber` exist.
- **Protocol is length-prefixed binary**, not JSON.  Simpler to parse in
  C than JSON, and our value shapes are small and fixed.

Still outstanding:

- **Array helpers.**  ✅ Shipped.  C foreign classes can call
  `st_arraySize(arr)` and `st_arrayAt(arr, index)`; indexes are the same
  zero-based indexes ServiceTalk arrays use.
- **Concurrent paired sends.**  ✅ Shipped.  `receivers parallelSend: selector
  withEach: arguments` sends one argument to each paired receiver concurrently,
  waits for every reply, and returns replies in receiver order.  The
  `parallelSend:withEach:timeoutMs:` form sets the deadline for every job and
  the enclosing collection call.  Pairing stops at the shorter array; invalid
  selectors, timeouts, or non-array arguments return `nil`.
- **Build portability.**  ✅ First pass shipped.  C uses
  `SMT_FOREIGN_CC`, `SMT_FOREIGN_CFLAGS`, `SMT_FOREIGN_LDFLAGS`; Rust uses
  `SMT_FOREIGN_CARGO`, `SMT_FOREIGN_RUST_TARGET`; both use
  `SMT_FOREIGN_TARGET_GOOS` / `SMT_FOREIGN_TARGET_GOARCH` for target naming.
- **Other languages.**  ✅ Exec engine shipped.  `(engine command args...)`
  plus `(emethod selector)` lets any language implement the stdin/stdout
  foreign protocol directly.
- **Sandboxing.**  ✅ Documented.  Foreign code has the same OS access as
  the ServiceTalk runtime; use container/user/filesystem/network policy for
  untrusted code.
- **Auto-generated .gitignore.**  ✅ Shipped.  `BuildForeignClass` writes
  a `.gitignore` (C: `_st.h` / `_st_runtime.c` / `_st_glue.c` / binary;
  Rust: `src/_st_main.rs` / `/target` / binary) on first build, and never
  clobbers an existing one.

## Multi-line REPL

✅ Bracket-balancing shipped.  `smalltalkComplete` (coordinator.go)
gathers continuation lines until `()`/`[]`/`{}` balance (skipping
strings and comments), with a `... > ` continuation prompt.  Blocks and
multi-statement / multi-line class definitions can now be typed or pasted
into `smt` / `smt repl`.

Still outstanding:

- Line-editing niceties: ✅ Shipped for terminal stdin.  Plain `smt repl`
  now uses `golang.org/x/term`; piped stdin keeps scanner mode.

## Smalltalk syntax gaps

The `.st` front end is intentionally minimal.  Things it does not yet
handle, roughly in order of user-visible value:

- **Cascades** — ✅ Shipped.  `obj foo; bar; baz` parses to a `(cascade
  recv …)` form (or a `(begin (print …) …)` fold for `Transcript show:`
  chains).  See README "Cascades".
- **Symbol literals** — ✅ Shipped.  `#foo` parses and evaluates to a String,
  and keyword-selector symbols like `#at:put:` lex as a single token (used by
  `perform: #at:put:`).  No distinct Symbol type — they're just strings.
- **Array literals** — ✅ Shipped.  `{1. 2. 3}` (dynamic) and `#(1 2 3)`
  (literal) both desugar to the `(array …)` form.  See README "Array and
  collection literals".
- **`super`** sends — ✅ Shipped.  Single inheritance via
  `class Child extends Parent` (`.st`) / `(superclass Parent)` (`.smt`);
  method lookup walks the chain, `super sel …` resumes lookup above the
  running method's class, inherited instance vars are present on the
  child, and cycles are detected at load time.  See README "Inheritance"
  and `Animal.st` / `Cat.st`.  `initialize` does not chain automatically
  — call `super initialize`.
- **Class-side methods** — outstanding, and a poor fit: a class isn't an
  object here, so running one means spawning a per-class singleton process
  to receive the message — and that process's instance vars would *be*
  class variables, i.e. mutable shared state (bottleneck + SPOF, or it
  diverges across workers). So: no class variables. If class methods ever
  land they stay stateless (factories / queries), or we skip them entirely.
- **`Transcript show:` is one-arg.**  Multi-arg print still requires `, `
  concat, but a `show:` cascade (`Transcript show: a; show: b; cr`) now folds
  to a run of prints — covers the common case.
- **Prefix `-ident`** — ✅ Shipped.  `-x` now compiles to `x negated`
  (joining the existing `-42` literal heuristic).

## Parity samples and README

- ✅ `Counter.st` (Smalltalk twin of `Counter.smt`) shipped; `Animal.st`
  / `Cat.st` / `Tiger.st` form the three-level inheritance example.
- ✅ README documents the Smalltalk syntax (class shape, calling
  conventions, inheritance) alongside the S-expression form.

## `smt object --verbose`

✅ Shipped.  `smt object` accepts `--verbose`; child processes still
inherit `SMT_VERBOSE=1` from the parent when spawned normally.

## Interactive environment roadmap — agreed 2026-10-26

Work from the top. Preserve system integration and separate objects/processes across languages and operating systems. Self-host as much of the GUI as possible. No private-interface restrictions.

1. **Method navigation and relationship graph — implemented.** Browse implementors, senders and source. Add draggable boxes with spring layout for method relationships, object pointers, lexical owners, roots and placement. Keep different edge meanings explicit.
2. **Assisted object workspace — implemented.** ObjectWorkspace supplies isolated scratch bindings, self, reusable it, property-read shortcuts, and a typed expression builder in the inspector.
3. **Reusable result objects — implemented.** Results retain identity; inspect them, pass them as arguments and build further operations from them.
4. **Object-specific views — implemented.** Head toward a full GUI with useful views supplied by objects.
5. **Observed types / LiveTyping — optional.** Keep available as a future option; no immediate priority.
6. **Record interactions as scripts/tests — implemented.** Turn useful exploration into repeatable work.
7. **Edit and resume debugging — native experiment implemented.** Pause a selected native method between statements; edit arguments/fields, replace the current statement, step or continue the held frame. Signals offer retry, supplied return or abort. Foreign methods keep their normal runtime execution.
8. **Message tracing — implemented.** Bounded per-object call/reply/signal records, chain following, object links, filters and JSON export; new objects join active tracing.
9. **Method change history — implemented through class-file history.** Source browser opens Git revisions, working diffs and editable definitions; copy historical source into the editor and commit the selected file using the Git object.
10. **Explicit recovery operations — implemented in debugger and wiring.** Failed native statements support retry/replacement, supplied return and abort. Wiring resumes from the failed node while retaining completed results.
11. **Clone and experiment — implemented.** Native clones have separate state storage and preserve loaded methods/superclasses across local and worker spawning. Referenced objects remain shared; resource-owning and foreign objects can supply their own clone method.
12. **Visual wiring / Fabrik — implemented.** Ordinary Wiring objects own typed message graphs. Connect output sockets to receiver/argument inputs, arrange boxes, run the graph and inspect results.
13. **Constraints between properties / ThingLab — deferred.** Record the idea; do not implement yet.
14. **Inspectable, editable GUI objects — implemented.** Workbench owns toolbar definitions and custom object-message tools; InspectorView, InteractionLog, Wiring and RunnableExample own their live GUI state. Their definitions can be edited through ordinary messages and inspectors; browser rendering remains the platform boundary.
15. **Semantic refactoring — selector rename implemented.** Parsed previews locate native definitions and sends, preserve bindings/comments/literals, flag literal references for review, and save selected files through the digest-checked source editor with Git history. Broader transformations remain suitable for AI assistance.
16. **Runnable documentation/examples — implemented.** Editable RunnableExample objects own isolated workspaces, executable checks and typed results. The Examples browser includes seven working demonstrations and supports adding examples.
17. **Inspectable promises — implemented.** Ordinary Promise objects retain the receiver, argument snapshot and typed result/error, expose state and await operations, and remain inspectable while calls run. Inspector messages and wiring runs can execute asynchronously.
18. **Changing behavior — native definition editor implemented.** Saved definitions apply to new object processes; old and new native instances coexist, verified with subprocess tests. Foreign source continues through its existing language/build tools; no live cross-language code replacement is assumed.
19. **Prototypes / class versions — native experiment implemented.** Derive an object's fields and method overrides without editing a class file; derived objects can be cloned again. Loaded definition digests distinguish coexisting behavior versions. No live foreign-runtime rewriting is assumed.
20. **Object mobility — research recorded in SPEC.** Native snapshot copies can spawn on workers. Preserving identity would need mailbox quiescence, subject/registry handoff, in-flight-call accounting and resource-specific transfer adapters. Arbitrary cross-language/OS migration remains outside this research-only item.
