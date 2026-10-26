# servicetalk — Distributed Smalltalk

Each Smalltalk object is an independent OS process with a NATS mailbox.
Message passing is NATS request/reply using object IDs as subjects.
The coordinator handles the registry, spawning, and transcript output.

```
./smt coordinator          # starts everything; spawns Main by default
./smt coordinator --main "" # coordinator only, no auto-spawn
```

---

## Run and debug

ServiceTalk uses NATS for object method calls.  Start a NATS server
first, or set `SMT_NATS_URL` to an existing broker:

```fish
docker run --rm -p 4222:4222 nats:2
set -x SMT_NATS_URL nats://127.0.0.1:4222
set -x SMT_CLUSTER default
./smt coordinator --main ""
```

NATS settings:

| Setting | Default | Use |
|---------|---------|-----|
| `SMT_NATS_URL` | `nats://127.0.0.1:4222` | Broker URL for all object method calls. |
| `SMT_CLUSTER` | `default` | Subject namespace. Use a different value for every cluster sharing one NATS broker; non-alphanumeric characters become `_`. |
| `SMT_NATS_STARTUP_TIMEOUT_MS` | `60000` | How long startup retries the initial broker connection before failing. |
| `SMT_SPAWN_TIMEOUT_MS` | `75000` | How long the coordinator/worker waits for a spawned object to register. Keep this longer than NATS startup timeout. |
| `SMT_MAILBOX_SIZE` | `1024` | Normal per-object FIFO depth. Calls beyond this return a mailbox-full signal instead of blocking. |
| `SMT_CONTROL_MAILBOX_SIZE` | `128` | Per-object control FIFO depth for `pulse:`, `class`, inspect, metrics, and `free`. |
| `SMT_NATS_MAX_CALL_BYTES` | `1048576` | Maximum encoded method-call request size sent through NATS. Keep this aligned with the broker's `max_payload`. |
| `SMT_INTERNAL_HTTP_MAX_BYTES` | `335544320` | Maximum body size for coordinator/worker internal HTTP JSON endpoints, including worker spawn requests with source bundles. |
| `SMT_SOURCE_BUNDLE_MAX_BYTES` | `268435456` | Maximum total bytes in the source bundle sent to a worker. Raise this for large foreign-class projects. |
| `SMT_SOURCE_BUNDLE_MAX_FILES` | `10000` | Maximum file count in the source bundle sent to a worker. Raise this for source-heavy bundles. |

The coordinator serves the browser UI at:

```text
http://localhost:8000/
```

The web UI has:

- an eval editor for Smalltalk or S-expression snippets,
- persistent workspace state between evals,
- captured `Transcript show:` output,
- a Live Objects table,
- object inspection for variables and methods.

Objects do not allocate TCP ports.  Each object registers a NATS subject
like `smt.<cluster>.obj.<object-id>.call`; all method calls go through
NATS request/reply.  The coordinator keeps HTTP for users and debugging.
Each object processes incoming method calls through a FIFO mailbox; a
call chain may re-enter the same object to avoid self-call deadlocks.

HTTP debug gateway:

```text
GET  /object/<id>/inspect       inspect variables/methods
POST /object/<id>/call          body: {"method":"value","args":[]}
POST /object/<id>/message       body: {"msgId":"debug","method":"value","args":[]}
POST /object/<id>/pulse         body: {"sweepId":123}
GET  /object/<id>/ping          liveness check
GET  /metrics                   runtime counters, spawn/pulse latency, object queue depths
```

NATS transport benchmark:

```fish
go test -run '^$' -bench '^BenchmarkNATSTraffic' -benchtime=1s -count=1
```

This starts an embedded NATS server and measures small method calls,
fan-out across many objects, pulse traffic, latency percentiles,
message rate, queue depth, fd count when available, and a 64 KiB
payload round trip.

The terminal REPL (`smt` / `smt repl`) is Smalltalk-only and reads
multi-line input: if a line leaves `(`, `[`, or `{` open, it shows a
`... > ` continuation prompt and keeps reading until brackets balance.
When stdin is a terminal it also has line editing and command history.

---

## Writing classes

A class can be defined three ways; `smt` searches each shape in turn:

| Shape           | Source                          | Language         |
|-----------------|---------------------------------|------------------|
| `Dog.st`        | Traditional Smalltalk           | ServiceTalk      |
| `Dog.smt`       | S-expression `(class …)`        | ServiceTalk      |
| `Dog/`          | Directory, manifest + foreign files | C, Rust, exec engine |

One class per file (or directory).  Both Smalltalk and S-expression
syntaxes parse to the same AST and run on the same evaluator — pick
whichever reads better for the class at hand.  The Smalltalk shape:

```smalltalk
class Dog [
    | name breed |

    name: n breed: b [
        name := n.
        breed := b
    ]

    bark [
        Transcript show: name , ' says: Woof!'
    ]

    name [ ^ name ]
]
```

- **`| name breed |`** between the class brackets declares instance
  variables; all start as `nil`.
- A method is `methodName [ body ]`.  Statements are separated by `.`,
  and the value of the last statement is the return value.
- **`^ expr`** exits the method early.  Inside a block, `^` exits the
  block (not the enclosing method — see *Blocks* below).
- A method name takes one of three syntactic forms:
  no-argument (`bark`), operator (`+`, `<`, `,`), or
  keyword-argument (`name:breed:`).  The keyword form interleaves
  the name parts with the values: `dog name: 'Rex' breed: 'Lab'`.
- `Transcript show: x` prints `x` to the coordinator transcript;
  `'…'` is a string literal; instance vars and method args are
  ordinary identifiers.

Foreign (C) classes are documented in their own section below.

The S-expression shape of the same class is in the *Lisp-style
syntax* section near the end.

---

## Inheritance

A class may declare a single superclass.  In Smalltalk syntax:

```smalltalk
class Animal [
    | name sound |
    initialize [ name := 'animal'. sound := '...' ]
    speak      [ ^ name , ' says ' , sound ]
    describe   [ ^ 'I am a ' , name ]
]

class Cat extends Animal [
    | tricks |
    initialize [
        super initialize.            "run Animal's initialize first"
        name := 'cat'.               "name/sound are inherited fields"
        sound := 'meow'.
        tricks := 0
    ]
    describe [ ^ (super describe) , ' that knows ' , tricks asString , ' tricks' ]
]
```

In S-expression syntax the parent is declared with a `(superclass …)`
form in the class body:

```scheme
(class Cat
  (superclass Animal)
  (vars tricks)
  (method describe () (concat (send super describe) " …")))
```

Semantics:

- **Method lookup** walks child → parent → grandparent; the first match
  wins, so a child method overrides the parent's.
- **`super sel …`** sends `sel` to the same object but starts lookup
  *above* the class whose method is running — the way an override
  delegates to the behaviour it shadows.  `super` is dispatched
  in-process (no extra round trip).
- **Instance variables** declared anywhere in the chain are all present
  on the object; a child method can read and write a parent's field.
- **`initialize` does not chain automatically** — a child that wants the
  parent's setup calls `super initialize` explicitly (as above).
- **Cycles** (`A extends B extends A`) are detected at load time and
  reported as an error rather than hanging.

See `Animal.st` / `Cat.st` for the worked example.  There is no
metaclass / class-side method support yet.

---

## Calling methods

```smalltalk
obj bark.                       "no-argument call"
obj at: 1.                      "one keyword arg — method name `at:`"
obj at: 1 put: 'hello'.         "two keyword args — method name `at:put:`"
obj from: 1 to: 5.              "two keyword args — method name `from:to:`"
1 + 2.                          "operator call"
arr first between: 1 and: 10.   "no-arg `first` then keyword `between:and:`"
```

The keyword form interleaves the name parts with the argument values:
`dict at: k put: v` calls a method named `at:put:` with arguments `k`
and `v`.

Calls on `self`:

```smalltalk
self at: key put: value.
self from: 1 to: 5
```

---

## Creating objects

`ClassName new` for a no-argument constructor, or any keyword call on a
capitalised name to spawn-and-initialize:

```smalltalk
d    := Dog name: 'Rex' breed: 'Labrador'.   "spawns Dog, calls initialize"
arr  := Array new.                            "built-in array"
dict := Dictionary new                        "built-in dictionary"
```

The runtime treats any keyword call on an identifier whose first
character is uppercase as a class instantiation — there are no static
methods on classes, so this is unambiguous.  `new` blocks until the
new process is registered and `initialize` has returned.

---

## Built-in forms

### Control flow

`ifTrue:`, `ifFalse:`, `ifTrue:ifFalse:` are methods on booleans;
`whileTrue:` / `whileFalse:` are methods on a (block) condition;
`repeat` loops a block forever; `timesRepeat:` and `to:do:` are methods
on numbers.

```smalltalk
cond ifTrue: [ doThing ].
cond ifTrue: [ doThing ] ifFalse: [ doOther ].

[ x < 10 ] whileTrue: [
    x := x + 1.
    Transcript show: x printString
].

[ x > 0 ] whileFalse: [ x := x + 1 ].

[ serveOneRequest ] repeat.                   "loops forever; escape by signalling in the body"

5 timesRepeat: [ Transcript show: 'tick' ].

1 to: 10 do: [i | Transcript show: i printString ].
0 to: 10 by: 2 do: [i | Transcript show: i printString ].

^ result.                                     "early return from method"
^ x + 1                                       "same"
```

### Assignment

```smalltalk
varName := expr     "writes the class's instance variable"
```

The variable must be declared in the class's `| name … |` field list
(or `(vars …)` in S-expression form).  Writing to an undeclared name
logs `set: undeclared field …; declared fields: […]` and returns `nil`
without touching state — auto-create is gone, the pulse-GC needs the
field list to be authoritative.  Method parameters and block
parameters are local to their call frame and need no declaration.

### Method calls

```smalltalk
target msg.               "no-argument call"
target kw: a kw2: b.      "keyword call"
ClassName kw: a.          "spawn + initialize"
[ x | x + 1 ]             "block literal — see Blocks"
```

### Cascades

A cascade sends several messages to the **same receiver**, separated by
`;`.  The receiver is the receiver of the message just before the first
`;`; the cascade's value is the last message's result.

```smalltalk
d := Dictionary new.
d at: 'x' put: 1; at: 'y' put: 2; at: 'z' put: 3.   "three at:put: on d"

c := Counter new.
c increment; increment; increment.                  "→ c, after 3 increments"

acc := Counter new; increment; increment; yourself. "cascade on `new`: messages go to the new instance"

Transcript show: 'a'; show: 'b'; cr.                "three transcript lines"
```

A `Transcript show: …; …` cascade folds into a run of prints (the
transcript is line-oriented, so each `show:` is its own line and `cr`
is a blank line).

When the head of a cascade is `ClassName new` (or any non-message
expression), the messages are sent to that expression's **result** — so
`Counter new; increment; yourself` increments the new instance and
`yourself` returns it.  (This deviates from strict Smalltalk, where the
cascade would target the class `Counter`; here `new` is a spawn and the
instance is the receiver you actually want.)

### Array and collection literals

```smalltalk
{ 1. 2. 3 }.                "dynamic array — elements are expressions,"
{ x + 1. y. z * 2 }.        "evaluated at runtime, collected into an Array"
#('a' 'b' 'c').             "literal array — numbers, strings, bare words"
#(foo bar true).            "bare words become strings; true/false/nil keep value"
```

`{ … }` elements are separated by `.` and may be any expression; `#( … )`
holds space-separated literals only.  Both produce an ordinary `Array`,
so all the `Array` methods below apply.

> **Put a space after the `.` separator.**  `.` is also a decimal point, so
> `{10.20.30}` lexes as the two floats `10.2` and `30` — write `{10. 20. 30}`
> for three integers.

### I/O

```smalltalk
Transcript show: anExpr.   "prints to the coordinator transcript"
```

### Arithmetic

`+`, `-`, `*`, `/`, `\\` (modulo), `abs`, `max:`, `min:`,
`floor`, `ceiling`, `sqrt`, `factorial`, `gcd:`, `between:and:`.

### Comparisons

`=`, `~=`, `<`, `>`, `<=`, `>=`.

### Logic

`not`, `and:`, `or:`, `xor:`.

### Strings

```smalltalk
s size.                                     "rune count"
s at: 0.                                    "0-based; nth rune as a string"
s copyFrom: start to: end.                  "0-based, half-open [start, end)"
s asUppercase.
s asLowercase.
s includes: 'needle'.                       "→ bool"
s startsWith: 'prefix'.                     "→ bool"
s endsWith: '.txt'.                         "→ bool"
s indexOf: 'needle'.                        "→ 0-based index or -1"
s splitOn: ','.                             "→ Array of string parts"
s lines.                                    "→ Array; trailing CRs stripped"
s asArray.    s chars.                      "→ Array of single-character strings"
s asCharacter.                              "→ first rune as a Character (see Characters)"
s reversed.                                 "→ rune-reversed string"
s capitalize.                               "→ first rune upper-cased"
s replaceAll: 'old' with: 'new'.
s repeat: 3.                                "→ string repeated N times ('ab' → 'ababab')"
s count: 'l'.                               "→ count of non-overlapping occurrences"
s padLeft: 5 with: '0'.                     "→ left-pad to width N ('42' → '00042')"
s padRight: 5 with: '.'.                    "→ right-pad to width N ('hi' → 'hi...')"
s printString.                              "→ single-quoted form ('hi'); embedded ' doubled"
s , other.                                  "concatenation; chains: a , b , c"
s trimSeparators.
url urlScheme.                              "→ 'https' etc., or nil if unparsable"
url urlHost.                                "→ 'example.com' or ''"
url urlPath.                                "→ '/foo/bar' or '/'"
s isEmpty.    s notEmpty.                    "→ bool"
s signal.                                   "raise the string as a signal — see Error handling"
```

The receiver string can also be treated as a **filesystem path**.  These
back the example shell and save you writing a separate `FileSystem`
class for the basics; errors come back as `nil` (probe with `fileExists`
first to tell "missing" from "unreadable"):

```smalltalk
path fileExists.                            "→ bool"
path isDirectory.   path isFile.            "→ bool"
path fileSize.                              "→ number of bytes, or nil"
path readText.                              "→ file contents as a string, or nil"
path dirEntries.                            "→ Array of basenames (sorted)"
path absolutePath.                          "→ absolute form against the cwd"
base joinPath: 'sub/file.txt'.              "→ joined path string"
name getenv.                                "→ value of $name, or nil"
```

### Characters

A `Character` holds one Unicode code point and is a primitive value (like a
number or boolean), not a process.  It is fully Unicode-aware — predicates
and case mapping work across the whole range, including non-BMP code points
such as `$😀`.

Construct one with the `$x` literal, `n asCharacter` (code point → Character),
or `aString asCharacter` (first rune).  There is no `Character value:` — the
runtime has no class objects, so `asCharacter` is the constructor.

```smalltalk
$a.                                         "character literal"
$ .                                         "the space character ($ then one space)"
$😀.                                        "non-BMP code points are fine"
97 asCharacter.                             "→ $a"
'hi' asCharacter.                           "→ $h (first rune)"
(s at: 0) asCharacter.                       "String at: gives a 1-rune string; bridge to a Character"

c asInteger.    c value.                    "→ Unicode code point ($a → 97)"
c asString.                                 "→ a one-rune string ($a → 'a')"
c printString.                              "→ the literal form ($a → '$a')"
c asUppercase.    c asLowercase.            "→ Character (Unicode case mapping)"
c isLetter.  c isDigit.  c isUppercase.     "→ bool"
c isLowercase.  c isSpace.  c isPunctuation.
$a < $b.   $a = $a.   $a ~= $b.             "ordered + compared by code point"
```

`String at:` / `asArray` / `chars` still return one-rune **strings** (not
Characters) for backward compatibility; use `asCharacter` to convert.

### JSON

```smalltalk
JSON encode: aDictionary.                   "→ JSON string"
JSON decode: '[1,2,3]'.                      "→ freshly-spawned Array"
```

`JSON` is a pseudo-class: `JSON encode:` / `JSON decode:` compile to the
evaluator's `jsonEncode` / `jsonDecode` forms, so they work the same in
`.st` method bodies, `.smt` files, and at the REPL.  `encode:` walks
Arrays and Dictionaries (and primitives) into a JSON string; `decode:`
parses JSON back into freshly-spawned Arrays / Dictionaries (returns
`nil` on parse failure, so a request handler can spot a bad body and
reply 400).

In the S-expression syntax the underlying forms are `(jsonEncode v)` and
`(jsonDecode s)` (see *Lisp-style syntax* below); `JSON encode:`/`decode:`
desugar to exactly these.

---

## Built-in classes

### Array

```smalltalk
arr := Array new.
arr add: 42.
arr add: 'hello'.
arr size.                                   "→ number"
arr at: 0.                                  "0-based; → value or nil"
arr at: 1 put: 'world'.
arr first.
arr last.
arr includes: 42.                           "→ bool"
arr isEmpty.    arr notEmpty.               "→ bool"
arr indexOf: 42.                            "→ 0-based index, or -1"
arr do: [elem | Transcript show: elem ].
arr collect: [x | x * 2 ].                  "→ new Array of mapped values"
arr select: [x | x > 10 ].                  "→ new Array of matching elements"
arr reject: [x | x > 10 ].                  "→ new Array of non-matching elements"
arr detect: [x | x > 10 ].                  "→ first match, or nil"
arr detect: [x | x > 10 ] ifNone: [0].      "→ first match, or the ifNone: block's value"
arr inject: 0 into: [acc x | acc + x ].     "→ fold (seed + 2-arg block)"
arr with: other do: [a b | … ].             "parallel iterate two arrays"
arr any: [x | x > 10 ].                     "→ bool; true if any element matches"
arr all: [x | x > 10 ].                     "→ bool; true if every element matches"
arr max.    arr min.                        "→ largest / smallest (default ordering); nil if empty"
arr join: ', '.                             "→ string of elements joined by the separator"
arr reversed.                               "→ new reversed Array"
arr copy.    arr asArray.                   "→ shallow copy"
arr addAll: other.                          "append another Array's elements"
arr remove: 42.                             "remove first match; → it, or nil"
arr removeAt: 0.                            "remove by 0-based index; → it, or nil"
arr sort.                                   "in-place ascending sort; → self"
arr sort: [a b | a > b ].                   "in-place sort with a comparator block; → self"
arr printAll.                               "prints each element to transcript"
```

`do:` / `collect:` / `select:` etc. evaluate the block once per element, and
**a block evaluation is a message to a block process** — fine for typical
collections, but for a tight numeric loop over a large range prefer
`1 to: n do: [i | … ]` (handled in-process) over building a big Array first.

### Dictionary

```smalltalk
d := Dictionary new.
d at: 'key' put: value.
d at: 'key'.                                "→ value or nil"
d at: 'key' ifAbsent: [42].                 "→ value, or block value if missing"
d at: 'key' ifAbsentPut: [42].              "→ value; if missing, stores+returns block value"
d at: 'key' ifPresent: [v | v * 2 ].        "→ block value if key present, else nil (block not run)"
d includesKey: 'key'.                       "→ bool"
d removeKey: 'key'.                         "→ removed value"
d size.
d isEmpty.    d notEmpty.                   "→ bool"
d do: [v | Transcript show: v ].
d keysAndValuesDo: [k v | Transcript show: k , '->' , v ].
d keys.                                     "→ new Array of keys"
d values.                                   "→ new Array of values"
d collect: [v | v * 2 ].                    "→ new Dictionary, same keys, mapped values"
d select: [v | v > 10 ].                    "→ new Dictionary of entries whose value matches"
d copy.                                     "→ shallow copy"
d printAll.                                 "prints all pairs to transcript"
```

### HttpClient

A built-in HTTP client.  Each instance is its own OS process; reuse one
client across many calls.  Every method returns a Dictionary with
`status` (number; 0 on transport error), `body` (string), `error`
(string or nil), and `headers` (a Dictionary of response headers, or nil
on a transport error).

```smalltalk
c := HttpClient new.
c get: 'https://example.com/'.                          "→ Dictionary"
c getWithTimeout: 'https://slow.example/' timeoutMs: 2000.
c getWithHeaders: 'https://example.com/' headers: headersDict.
c post: 'https://example.com/api' body: 'raw bytes'.
c post: 'https://example.com/api'
   body: '{"x":1}'
   contentType: 'application/json'.
c put: 'https://example.com/api' body: 'raw bytes'.
c put: 'https://example.com/api'
   body: '{"x":2}'
   contentType: 'application/json'.
c patch: 'https://example.com/api'
   body: '{"x":3}'
   contentType: 'application/json'.
c delete: 'https://example.com/api'.
```

The older `get:timeoutMs:` / `get:headers:` spellings still work as
aliases for `getWithTimeout:timeoutMs:` / `getWithHeaders:headers:`.

### HttpServer

Built-in HTTP listener.  Each instance owns one net/http listener and
forwards every incoming request to a user-supplied handler object as a
synchronous method call.

```smalltalk
s := HttpServer new.
s listenOn: 8080 handler: myHandler.                            "non-blocking"
s listenOn: 8080 handler: myHandler methodName: 'handle:'.
s isRunning.                                                    "→ bool"
s port.                                                         "→ listening port number"
s stop.
s free.                                                         "stop and deregister the server"
```

The handler must implement `handle:` (or whatever method name you
passed to `listenOn:handler:methodName:`) and return a Dictionary
containing
`status` (number, default 200), `body` (string), and optional
`contentType` (default `application/json`).  The request Dictionary
carries `method`, `path`, `query`, and `body` keys.

```smalltalk
class MyHandler [
    handle: req [
        resp := Dictionary new.
        resp at: 'status' put: 200.
        resp at: 'body' put: 'you asked for ' , (req at: 'path').
        ^ resp
    ]
]
```

### RobotsMatcher

A robots.txt parser + matcher.  Reused across many lookups; one
instance per service is enough.  Implements User-agent / Allow /
Disallow / Crawl-delay grouping with longest-prefix-match (no
wildcards or `$` anchors yet).

```smalltalk
m := RobotsMatcher new.
m parse: robotsTxtBody.                              "rebuilds state"
m allows: 'https://example.com/admin' agent: 'MyBot'. "→ bool"
m crawlDelayFor: 'MyBot'.                            "→ number-of-seconds or nil"
m agentCount.                                        "→ diagnostic"
```

---

## Blocks

A block is a closure: it captures the method arguments at the point of creation
and holds a live reference to its parent object for variable access.

```smalltalk
b := [x | Transcript show: 'got ' , x printString ].

b value: 42.        "→ prints 'got 42'"
b free.             "explicit shutdown (optional; pulse GC handles it too)"
```

Block-literal forms:

```smalltalk
[ 42 ]                  "no args; evaluates body, returns its value"
[ x | x + 1 ]           "one arg"
[ x y | x + y ]         "two args"
```

The Smalltalk-80 `[:x | … ]` form (with leading colons on each arg)
also parses, for muscle-memory; the `|` already separates params from
body, so the colons add nothing.

### Calling a block by parameter name

`value`, `value:`, `value:value:`, `value:value:value:` always work
positionally (a block passed to `arr do:` or any other consumer
expecting positional invocation still fits).  In addition, you can
call a block using its parameter names as the keywords:

```smalltalk
[ c | c + 1 ] c: 41.                   "→ 42"
[ x y | x + y ] x: 3 y: 4.             "→ 7"
[ x y | x - y ] y: 3 x: 10.            "→ 7   (any keyword order)"
```

Keywords can appear in any order — the runtime reorders the args to
match the block's declared parameter list.  A keyword that doesn't
match a parameter, a missing parameter, or an arity mismatch fails as
`block doesNotUnderstand: <selector>` and returns nil.

### Variable access in blocks

- Reading a variable: checks captured args first, then fetches from the parent object via RPC.
- Writing with `name := val`: writes directly to the parent object's instance variable via RPC.
- `self` inside a block refers to the **parent object**, not the block process.

```smalltalk
class Accumulator [
    | total |

    initialize [ total := 0 ]

    sumAll: arr [
        arr do: [x | total := total + x ].
        ^ total
    ]
]
```

### `^` inside a block

`^ expr` **returns from the block**, not from the method that created
it.  This matches how most people use blocks anyway, and avoids the
complexity of cross-process non-local returns.

If you need to accumulate a result across a block iteration, use an
instance variable as an accumulator (declare it in the class's field
list):

```smalltalk
class Finder [
    | result |

    firstOver: n in: arr [
        result := nil.
        arr do: [x |
            (result isNil and: [x > n]) ifTrue: [ result := x ]
        ].
        ^ result
    ]
]
```

### Block lifecycle

Blocks are OS processes.  They exit when no live reference still pulses
them — see "Pulse-based GC" below.  You can also release one explicitly
with `b free`.

---

## Object lifecycle

- **Coordinator** (port 8000 by default): registry, spawning,
  transcript, root-set, pulse driver.
- **Objects** do not listen on TCP ports.  Each object registers a NATS
  subject like `smt.<cluster>.obj.<object-id>.call`.
- **Objects process calls through FIFO mailboxes.**  Normal calls use
  `SMT_MAILBOX_SIZE`; control calls (`pulse:`, `class`, inspection,
  metrics, `free`) use `SMT_CONTROL_MAILBOX_SIZE` and are drained first.
  A full mailbox returns a signal to the caller immediately.  The runtime
  does not retry it automatically; callers that want retry/backoff should
  implement that policy at the application level.
- Liveness is reachability-based: the coordinator pulses the
  root-set every `SMT_PULSE_INTERVAL_MS` (default 10s); roots
  propagate the sweep id to their references, those propagate to
  theirs, and an object that goes longer than
  `SMT_PULSE_TIMEOUT_SECS` (default 60s) without seeing a fresher
  sweep id self-deregisters and exits.  See *Pulse-based GC* below
  for the full mechanism.
- Objects do **not** send periodic heartbeats to the coordinator —
  fanning that in from 20K objects every few seconds would melt
  the coordinator.  Instead, the coordinator runs a **slow
  stale-prune sweep**: every `SMT_STALE_PRUNE_INTERVAL_MS`
  (default 1s) it sends one liveness request; if the request fails,
  the entry is evicted.  At 20K entries that's a ~5.5h full
  pass — slow enough that coordinator load is flat, fast enough
  that hard-crashed objects don't accumulate forever.  Tune via
  the env var if your churn is faster.

### Pulse-based GC

Objects die when no live reference reaches them.  The coordinator owns a
small set of **roots** (the workspace, the `--main` class) and every
`SMT_PULSE_INTERVAL_MS` (default 10 s) sends `pulse:` with a fresh sweep
id (current `time.UnixMilli`) to each root.  Receivers update their
`lastSweepID` and forward the same id to every object they hold —
`instanceVars`, `arrayData`, `dictionaryData`, captured block args.  An
object that goes longer than `SMT_PULSE_TIMEOUT_SECS` (default 60 s)
without receiving a strictly-newer sweep id deregisters and exits.

Cycles fall out for free: once `A` propagates sweep id `t` to `B`, `B`
back to `A` finds `A.lastSweepID == t` and stops there.

To override the default field walker — for foreign classes that hold
refs in C state, or for ST classes with custom storage — define `pulse:`
yourself:

```smalltalk
class CacheBox [
    | hot cold |

    pulse: sweepId [
        hot pulse: sweepId          "hot stays alive; cold expires"
    ]
]
```

Foreign classes are no-op by default (the runtime can't see C-side
state); add `(cmethod pulse: my_pulse)` and call `st_send(ref, "pulse:",
1, args)` for each ref you want to keep alive.

### Registry persistence (optional)

The registry is in-memory by default, so a coordinator restart loses
every object address and the cluster has to come back up from scratch.
Pass `--registry-file PATH` (or set `SMT_REGISTRY_FILE`) to have the
coordinator snapshot the registry — and the pulse-root set — to disk
shortly after any change, and **reload + revalidate** it on startup:

```bash
./smt coordinator --main "" --registry-file /var/lib/smt/registry.json
```

On restart the coordinator pings each persisted object and re-adopts the
ones that still answer (re-pinning any that were roots), dropping the
rest.  This lets a coordinator that crashed — or was restarted — re-adopt
objects that outlived it (objects on other machines, or a crash that
didn't take the local child processes down) instead of forcing a
whole-cluster teardown.  It does **not** make the coordinator
highly-available: while it's down, spawns and cross-object calls that
need a registry lookup still fail.

---

## Numbers as objects

Numbers have methods directly — no boxing or spawning:

```smalltalk
5 factorial.            "→ 120"
17 isPrime.             "→ true"
144 sqrt.               "→ 12"
-7 abs.                 "→ 7"
3.7 ceiling.            "→ 4"
10 gcd: 25.             "→ 5"

"Arithmetic"
10 + 5.                 "→ 15"
10 ** 3.                "→ 1000"
10 \\ 3.                "→ 1  (modulo)"

"Comparison"
5 between: 1 and: 10.   "→ true"
5 max: 9.               "→ 9"

"Iteration — returns Array of [start..end]"
1 to: 5.

"Iteration with a block"
1 to: 5 do: [i | Transcript show: i printString ].

"Step iteration"
0 to: 10 by: 2 do: [i | Transcript show: i printString ].   "0 2 4 6 8 10"

"Number class"
5 class.                "→ 'Number'"
```

### Custom Number methods — `Number.st`

Put a `Number.st` file on the classpath to extend numbers:

```smalltalk
class Number [
    double [
        ^ self * 2
    ]

    digits [
        "count decimal digits"
        ^ self = 0
            ifTrue: [ 1 ]
            ifFalse: [ 1 + (self abs // 10) digits ]
    ]
]
```

`Number.st` (or `Number.smt`) is loaded lazily on first use.  Built-in
methods always take precedence over a user-defined override of the
same name.

## Coordinator as an object

The symbol `coordinator` resolves to the coordinator object.  It implements:

```smalltalk
coordinator allObjects.                  "→ Array of all live object IDs"
coordinator objectsOfClass: 'Counter'.   "→ Array of Counter IDs"
coordinator objectAt: 'Counter'.         "→ first Counter ID, or nil"
coordinator classNameOf: anId.           "→ class name string, or nil"
coordinator subjectOf: anId.             "→ NATS subject string"
coordinator size.                        "→ count of live objects"
coordinator includesObject: anId.        "→ bool"
coordinator spawn: 'Counter'.            "→ new Counter ID (no initialize)"
coordinator deregister: anId.            "remove from registry"
coordinator printRegistry.               "print all objects to transcript"
coordinator pin: anId.                   "make anId a GC root; → anId"
coordinator unpin: anId.                 "drop it from the root set; → anId"
coordinator isPinned: anId.              "→ bool"
coordinator class.                       "→ '__coordinator__'"
```

`pin:` is the escape hatch for **long-lived service objects** — an
`HttpServer`, a connection pool, a cache — that nothing in the workspace
references but that you want kept alive.  A pinned object becomes a
pulse-GC root: it (and everything it transitively references) survives
even when no other live reference reaches it.  `unpin:` removes it again,
after which its subtree starts dying on the next pulse-timeout window.

```smalltalk
srv := coordinator pin: HttpServer new.   "daemon stays up across GC sweeps"
```

Example — print the class of every live object:

```smalltalk
coordinator allObjects do: [o |
    Transcript show: (coordinator classNameOf: o)
].
```

---

## Universal object methods

Every value — objects, numbers, strings, booleans, **and the literal `nil`** —
implements:

```smalltalk
obj class.                  "→ class name string"
obj yourself.               "→ obj (identity)"
obj isNil.                  "→ bool (true only for the literal nil)"
obj notNil.                 "→ bool"
obj inspect.                "prints all instance variables to transcript"
obj respondsTo: 'method'.   "→ bool"
obj = other.                "→ bool; identity (same objid). nil = nil is true"
obj ~= other.               "→ bool; identity inequality"
obj signal.                 "raise obj as a signal — caught by onError: (see Error handling)"
obj perform: 'bark'.                         "reflective call — same as obj bark"
obj perform: #at:put: with: k with: v.       "with up to three args"
obj perform: #at:put: withArguments: {k. v}. "args taken from an Array"
```

`perform:` resolves the selector string at runtime and re-enters normal
dispatch, so it reaches user methods, built-ins, and `doesNotUnderstand:`
alike — this is what makes a forwarding `doesNotUnderstand:` (see below)
able to actually re-send the message.  It works on numbers, strings,
booleans, and `nil` too (`5 perform: #factorial`).

`nil` dispatches as a real value (see `nil.go`), so `nil isNil` is
`true`, `42 isNil` is `false`, etc. without exceptions or "target is
not an object" errors.

---

## doesNotUnderstand:

Define `doesNotUnderstand:` in any class to intercept calls to unknown methods:

```smalltalk
class Proxy [
    | target |

    target: t [ target := t ]

    doesNotUnderstand: msg [
        Transcript show: 'Proxy forwarding: ' , msg.
        ^ target perform: msg          "hypothetical"
    ]
]
```

If `doesNotUnderstand:` is not defined either, the unknown method name
is logged to the transcript and `nil` is returned to the caller.

---

## Error handling

Raise an error by sending `signal` to a string (the message), and catch
it by protecting a block.  There are no exception *classes* — a signal
carries a string message, and the handler block receives that string.

```smalltalk
'file not found' signal.                     "raise — unwinds to the nearest handler"

[ risky ] onError: [e | Transcript show: 'caught: ' , e].
[ risky ] on: 'Error' do: [e | Transcript show: 'caught: ' , e].  "on: value is an ignored label"

[ openFile ] ensure: [ closeFile ].          "cleanup always runs, signal or not"
```

- `[risky] onError: handler` runs the risky block; if it signals, the
  handler block is called with the message string.  The cascade returns
  the risky block's value on success, or the handler's value on error.
  `on:do:` is the same, with a leading placeholder argument for
  muscle-memory (`[…] on: Error do: […]`) — the dialect ignores it.
- `[body] ensure: cleanup` always runs the cleanup block afterward,
  whether or not `body` signalled; the signal still propagates.
- An **unhandled** signal is logged to the transcript and the call
  returns `nil`.
- Foreign (C / Rust) classes raise the same signal from native code —
  `st_signal("msg")` in C, `signal("msg")` in Rust — and it is caught by
  the same `onError:` / `ensure:` on the ST side.

---

## Lisp-style syntax

Every example above can also be written in S-expression form.  Files
ending `.smt` are parsed by the Lisp-style parser; files ending
`.st` use the Smalltalk parser; both produce the same AST and the
same evaluator runs them.  The web REPL and the terminal REPL
auto-route per line: input that starts with `(` (or `;` for a
comment) goes to the S-expr parser, anything else to the Smalltalk
parser, so you can mix freely at the REPL.

The Lisp-style syntax is useful in three places where Smalltalk gets
awkward:

- **Foreign-class manifests** — the C-binding glue file is always
  `(foreign-class …)` (see *Foreign classes (C)* below).
- **One-shot evals** at a shell prompt where matching Smalltalk
  brackets through `''` quoting is annoying:
  `./smt --eval '(send 5 factorial)'`.
- **Forms that aren't method calls** — `(jsonEncode v)`,
  `(jsonDecode s)`, `(concat a b c)`, `(if cond then else)`.  These
  exist as top-level S-expr forms in the evaluator; the Smalltalk
  parser has no equivalent function-call notation.

### Rosetta stone

| Concept                      | Smalltalk                                      | S-expression                                  |
|------------------------------|------------------------------------------------|-----------------------------------------------|
| Number / string literal      | `42`,  `'hi'`                                  | `42`, `"hi"`                                  |
| Identifier                   | `name`                                         | `name`                                        |
| Assignment                   | `x := 1`                                       | `(set x 1)`                                   |
| No-arg method call           | `obj bark`                                     | `(send obj bark)`                             |
| Operator method call         | `1 + 2`                                        | `(send 1 + 2)`                                |
| Keyword method call          | `dict at: k put: v`                            | `(send dict at:put: k v)`                     |
| Call on self                 | `self foo`, `self at: k put: v`                | `(foo)` or `(at: k put: v)`                   |
| Spawn (no init args)         | `Counter new`                                  | `(new Counter)`                               |
| Spawn (with init args)       | `Dog name: 'Rex' breed: 'Lab'`                 | `(new Dog "Rex" "Lab")`                       |
| Block, no args               | `[ 42 ]`                                       | `(block () 42)`                               |
| Block, args                  | `[x y | x + y]`                              | `(block (x y) (send x + y))`                  |
| Statement separator          | `.`                                            | (newline / next list)                         |
| Return                       | `^ expr`                                       | `(^ expr)` or `(return expr)`                 |
| Conditional                  | `cond ifTrue: [a] ifFalse: [b]`                | `(if cond a b)`                               |
| While loop                   | `[cond] whileTrue: [body]`                     | (use `whileTrue:` on a block)                 |
| Print                        | `Transcript show: x`                           | `(print x)`                                   |
| Concatenate                  | `a , b , c`                                    | `(concat a b c)`                              |
| JSON encode / decode         | (use S-expr form)                              | `(jsonEncode v)`,  `(jsonDecode s)`           |

### Class definition, side by side

The same `Dog` class in both syntaxes:

```smalltalk
class Dog [
    | name breed |

    name: n breed: b [
        name := n.
        breed := b
    ]

    bark [
        Transcript show: name , ' says: Woof!'
    ]

    name [ ^ name ]
]
```

```scheme
(class Dog
  (vars name breed)

  (method name:breed: (n b)
    (set name n)
    (set breed b))

  (method bark ()
    (print name "says: Woof!"))

  (method name ()
    name))
```

### Method calls

```scheme
(send obj bark)                          ; no-argument call
(send 1 + 2)                             ; operator call
(send dict at: k put: v)                 ; keyword pairs combined into at:put:
(send dict at:put: k v)                  ; same name pre-joined
(send arr first)                         ; chained — call first, then call on its result
(send (send arr first) between:and: 1 10)
```

Implicit self-calls (no `(send self …)` wrapper):

```scheme
(at: key put: value)                     ; calls at:put: on self
(from: 1 to: 5)                          ; calls from:to: on self
```

### Spawning

```scheme
(set d    (new Dog "Rex" "Lab"))         ; spawns Dog, runs (initialize "Rex" "Lab")
(set arr  (new Array))
(set dict (new Dictionary))
(set rc   (new HttpClient))
```

### Blocks

```scheme
(set add1 (block (x) (send x + 1)))
(send add1 value: 41)                    ; → 42

(set greet (block ()
  (print "hi")))
(send greet value)                       ; → prints "hi"

(send arr do: (block (x)
  (print x)))                            ; iterate
```

### Control flow

```scheme
(if cond
  (then-expr)
  (else-expr))

(send 5 timesRepeat: (block ()
  (print "tick")))

(send 1 to:do: 10 (block (i)
  (print i)))

(send 0 to:by:do: 10 2 (block (i)
  (print i)))                            ; 0 2 4 6 8 10

(send (block () (send i < 10)) whileTrue:
  (block ()
    (set i (send i + 1))
    (print i)))
```

`(if cond then else)` is a special form, not a method call — the
`else` branch is optional.  All the iterators above are plain method
calls on numbers / blocks / arrays.

### Top-level forms with no Smalltalk equivalent

```scheme
(concat "Hello, " name "!")              ; → joined string
(jsonEncode aDictionary)                 ; → JSON string
(jsonDecode "[1,2,3]")                   ; → freshly-spawned Array
```

### A small worked example

A two-class S-expression program — `Counter.smt` plus a one-liner —
and the same logic in Smalltalk:

```scheme
; Counter.smt
(class Counter
  (vars count)
  (method initialize ()  (set count 0))
  (method increment ()   (set count (send count + 1)))
  (method value ()       count))
```

```bash
./smt --eval '(set c (new Counter)) (send c increment) (send c increment) (send c value)'
; → 2
```

```smalltalk
"Counter.st"
class Counter [
    | count |
    initialize [ count := 0 ]
    increment  [ count := count + 1 ]
    value      [ ^ count ]
]
```

```bash
./smt --eval "c := Counter new. c increment. c increment. c value"
# → 2
```

Both produce a `Counter` class on the same evaluator; an `.st` Counter
and a `.smt` Counter would shadow each other (the loader stops at the
first match), but a class written in one syntax can freely call
methods on a class written in the other.

---

## Foreign classes (C)

A class can be implemented in C instead of `.smt`.  Put it in a directory
named after the class:

```
CCounter/
  CCounter.smt     ; (foreign-class CCounter (cmethod name c-func) ...)
  ccounter.c       ; user code — implements each cmethod
```

The manifest lists each method's ST name and the C function it
dispatches to:

```scheme
(foreign-class CCounter
  (cmethod initialize  ccounter_initialize)
  (cmethod increment   ccounter_increment)
  (cmethod value       ccounter_value)
  (cmethod add:        ccounter_add))
```

Each C function has the same signature:

```c
#include "_st.h"
Value ccounter_add(int nargs, Value* args) {
    if (nargs < 1 || !st_isNumber(args[0]))
        st_signal("CCounter add: expects a number");
    /* ... */
    return st_number(/* ... */);
}
```

On first `(new CCounter)` (or any change to `.c` / `.h` / `.smt` files in
the directory), `smt` generates `_st.h`, `_st_runtime.c`, and `_st_glue.c`
alongside your code and invokes the C compiler to build the class binary.
Each instance of the class runs as its own subprocess behind the usual
object transport — indistinguishable from a native ST class to callers.
Use `st_send` from C to call methods on other objects; `st_signal` raises
an exception the ST side can catch via `on:do:`.

C build settings:

| Setting | Default | Use |
|---------|---------|-----|
| `SMT_FOREIGN_CC` | `$CC`, then `cc` | C compiler command. |
| `SMT_FOREIGN_CFLAGS` | empty | Extra compiler flags inserted before the normal output/source args. |
| `SMT_FOREIGN_LDFLAGS` | empty | Extra linker flags appended after source args. |
| `SMT_FOREIGN_TARGET_GOOS` | `$GOOS`, then current OS | Target OS used for artifact naming; `windows` produces `.exe`. |
| `SMT_FOREIGN_TARGET_GOARCH` | `$GOARCH`, then current arch | Target arch label for packaging/toolchain selection. |

No automatic type coercion.  `Value` carries the ST-native type tag;
predicates (`st_isNumber`, `st_isString`, `st_isObject`) and accessors
(`st_asNumber`, `st_bytes`) do the unwrapping explicitly.

See `CCounter/` for a complete example.

---

## Foreign classes (Rust)

A foreign class can be implemented in Rust instead of C.  The directory
layout is a normal Cargo crate, and the manifest uses `rmethod` (rather
than `cmethod`) to map each ServiceTalk selector to a Rust function:

```
RCounter/
  RCounter.smt     ; (foreign-class RCounter (rmethod selector rust_fn) ...)
  Cargo.toml       ; crate manifest — see below
  src/
    lib.rs         ; your code: one `pub fn` per method
    _st_main.rs    ; auto-generated glue (do not edit; gitignored)
```

The manifest:

```scheme
(foreign-class RCounter
  (rmethod initialize  initialize)
  (rmethod increment   increment)
  (rmethod value       value)
  (rmethod add:        add)
  (rmethod classOf:    class_of))
```

Each method is a `pub fn` in `src/lib.rs` taking `&[Value]` and returning
`Value`:

```rust
use servicetalk::{send, signal, Value};

pub fn add(args: &[Value]) -> Value {
    if args.is_empty() || !args[0].is_number() {
        signal("RCounter add: expects a number");   // → caught by onError:/ensure:
    }
    // ... mutate state ...
    Value::number(/* ... */)
}

// Call back into the cluster with `send`:
pub fn class_of(args: &[Value]) -> Value {
    send(&args[0], "class", &[])
}
```

`Cargo.toml` declares the runtime path-dependency and a `[[bin]]` named
after the class whose `path` is the generated glue file (`lib.rs` is
auto-detected by Cargo):

```toml
[package]
name = "rcounter"
edition = "2021"

[[bin]]
name = "RCounter"            # MUST match the class name
path = "src/_st_main.rs"     # generated by BuildForeignClass

[dependencies]
servicetalk = { path = "../servicetalk-rs" }
```

On first `(new RCounter)` (or any change under `src/` or to
`Cargo.toml`), `smt` regenerates `src/_st_main.rs`, runs `cargo build
--release --bin RCounter`, and copies the binary up to `RCounter/RCounter`
— after which it behaves exactly like a C foreign class or a native ST
class to callers.  `Value` carries the ST type tag; use the predicates
(`is_number`, `is_string`, `is_object`) and accessors (`as_number`,
`as_bool`) to unwrap, and the constructors (`Value::number`,
`Value::cstr`, `Value::objid`, `Value::Nil`) to build results.

Rust build settings:

| Setting | Default | Use |
|---------|---------|-----|
| `SMT_FOREIGN_CARGO` | `$CARGO`, then `cargo` | Cargo command. |
| `SMT_FOREIGN_RUST_TARGET` | empty | Passed as `cargo build --target TARGET`; output is read from `target/TARGET/release/`. |
| `SMT_FOREIGN_TARGET_GOOS` | `$GOOS`, then current OS | Target OS used for copied artifact naming; `windows` produces `.exe`. |
| `SMT_FOREIGN_TARGET_GOARCH` | `$GOARCH`, then current arch | Target arch label for packaging/toolchain selection. |

The same caveats as C apply: foreign code runs with full OS access, and a
class holding object references in Rust state must implement `pulse:`
(an `(rmethod pulse: …)`) or pulse-GC can't keep its references alive —
`smt` prints a build-time warning when a foreign class calls `send` but
declares no `pulse:`.

See `RCounter/` for a complete worked example, and `Robocop/`,
`ClickHouse/`, `DynamoDB/`, `S3/` for foreign classes that wrap real
Rust crates.

## Foreign classes (exec engine)

Any language can implement the foreign subprocess protocol directly.
Use `(engine ...)` to name the command and `(emethod selector)` to declare
which ServiceTalk method names it accepts:

```scheme
(foreign-class PyThing
  (engine python3 worker.py "{class}")
  (emethod initialize)
  (emethod value)
  (emethod pulse:))
```

The runtime starts the engine command in the class directory and talks to
it over the same length-prefixed stdin/stdout protocol used by C and Rust.
Placeholders in engine args:

| Placeholder | Expands to |
|-------------|------------|
| `{dir}` | Absolute class directory. |
| `{class}` | Class name without `.exe`. |
| `{binary}` | Target artifact path for the current `SMT_FOREIGN_TARGET_GOOS`. |

No build step is run for exec-engine classes.  Ship the script/runtime
files in the foreign-class directory and make sure the named interpreter
or executable is present on every worker that may spawn the class.

Foreign class trust model: C, Rust, and exec-engine code run as ordinary
subprocesses with the same OS access as the ServiceTalk runtime.  They are
not sandboxed by ServiceTalk; use container, user, filesystem, and network
policy boundaries when running untrusted code.

---

## Running the demo

```bash
go build -o smt .
./smt coordinator          # spawns Main, prints to stdout
```

Class files (`*.smt`, `*.st`, foreign-class dirs) are looked up in this
order:

1. Current working directory.
2. The directory holding the running `smt` binary.
3. The parent of that directory (covers the `bundle/bin/smt` →
   `bundle/Main.smt` shape produced by `bundle.sh`).
4. `<binary's parent>/Resources` (covers macOS `.app` bundles where
   classes are conventionally under `Contents/Resources/`).
5. Anything in `SMT_PATH` (`PATH`-style — `:` on Unix, `;` on Windows).
6. Anything passed via `--path DIR` (repeatable).

The first match wins.  The binary-relative entries mean a packaged
app — including a double-clicked macOS `.app` whose CWD is `/` — finds
its classes without `--path`.

```bash
./smt coordinator --path /opt/myapp                   # single extra dir
SMT_PATH=/opt/myapp:/opt/shared ./smt coordinator     # PATH-style
```

The coordinator prints class names in the transcript, not raw object IDs.

```
[Main] Hello from the main object
[Counter] Counter value: 5
[__block__]   item: 10
```

To suppress auto-spawn and control everything manually:

```bash
./smt coordinator --main ""
# in another terminal:
./smt object --class Counter --id myCounter --coordinator http://localhost:8000 --init
```

---

## Writing an app

A servicetalk **app** is a directory containing class files.  Nothing
else.  No `package.json`, no manifest, no entry-point declaration —
the runtime just looks for class files matching whatever class name
the cluster is asked to spawn.

```
myapp/
  Main.smt           # the entry-point class (by convention)
  Counter.smt        # one class per file; filename = class name
  Greeter.st         # traditional Smalltalk syntax also OK
  CCounter/          # foreign C class — directory of the same name
    CCounter.smt     # manifest: (foreign-class CCounter (cmethod ...))
    ccounter.c       # implementations
  Number.smt         # OPTIONAL: extends numbers with custom methods
                     # (loaded lazily on first use)
```

### Conventions

- **One class per file.**  The class declared inside `Foo.smt` (or
  `Foo.st`, or `Foo/Foo.smt`) must be named `Foo` — `LoadClass`
  searches by filename.
- **`Main.smt` is the entry point** if you launch the cluster with
  `coordinator --main Main`.  The runtime spawns one instance and
  calls `initialize` on it.  Everything else follows from there.
- **Order doesn't matter.**  Class files are loaded lazily on first
  spawn.  You can add a new class file at runtime; the next `(new
  Foo)` will pick it up.
- **`Number.smt` is special** — it's loaded lazily the first time
  numeric dispatch hits a non-built-in method name, and lets you add
  methods to numbers without spawning anything.  See *Numbers as
  objects* above.
- **Blocks aren't files.**  Block closures are synthesised by the
  runtime; you never write a `Block.smt`.
- **Foreign classes** are documented in *Foreign classes (C)* above —
  the directory name, the manifest filename, and the class name all
  match.

### What goes where

| Files in your app                | Why                                           |
|----------------------------------|-----------------------------------------------|
| `Main.smt`                       | Entry point — the class the cluster spawns first |
| `OtherClass.smt`, `OtherClass.st`| Each class your app uses                      |
| `ForeignClass/ForeignClass.smt`  | Manifest for a C-backed class                 |
| `ForeignClass/*.c`               | C implementations                             |
| `Number.smt`                     | Optional: custom methods on numbers           |

### Testing locally

The runtime is one binary; point it at your app dir with `--path` (or
just `cd` into it):

```bash
go build -o smt .                                 # build the runtime once
./smt coordinator --path path/to/myapp --main Main
# or
cd path/to/myapp && /path/to/smt coordinator --main Main
```

Once it's running, hit the web REPL at `http://localhost:8000/`:

```smalltalk
m := Main new.                "spawn your entry-point class"
c := Counter new.             "or any other class"
c increment.
c value.                      "→ 1"
coordinator allObjects        "see what's alive"
```

Iterate by editing `.st` (or `.smt`) files and spawning new instances —
existing objects keep their old class definition until they exit; new
spawns pick up the change.

### Local cluster (one machine, multiple workers)

Use this to mirror the production shape on a laptop:

```bash
# terminal 1 — coordinator
./smt coordinator --path path/to/myapp --main "" --port 8000

# terminal 2 — worker A
SMT_HOST=127.0.0.1 ./smt worker \
    --path path/to/myapp \
    --coordinator http://localhost:8000 \
    --port 8001

# terminal 3 — worker B
SMT_HOST=127.0.0.1 ./smt worker \
    --path path/to/myapp \
    --coordinator http://localhost:8000 \
    --port 8002
```

Now `(new Counter)` from the web REPL lands on a randomly-chosen
worker; you can validate cross-worker method calls, pulse-GC behaviour,
and worker eviction without touching containers or Kubernetes.

---

## Packaging for deployment

There are three ways to ship an app, from lightest to heaviest:

### `smt run` — run a directory as an app

```bash
smt run path/to/your/app                 # classpath = app dir, spawns Main, serves
smt run path/to/your/app --main Boot     # different entry class
smt run path/to/your/app --eval 'Counter new value'
```

`smt run DIR` is shorthand for `smt coordinator --path DIR --main Main`: it
puts the directory's classes first on the classpath, spawns the entry class
(`Main` by default), and serves (web REPL + cluster). No build step — the
`smt` binary and the app directory travel together.

### `smt build` — one self-contained binary

```bash
smt build path/to/your/app -o yourapp    # bakes the app's classes into a binary
./yourapp                                # runs Main and serves — no class files needed
./yourapp --eval 'Counter new value'     # also works; classes are embedded
./yourapp repl                           # explicit subcommands still work
```

`smt build` embeds the app's `.smt` / `.st` class files into a fresh copy of
the runtime via `go:embed`, producing a single file you can copy and run
anywhere of the same OS/arch. The built binary auto-spawns `--main` (default
`Main`) when run with no subcommand. Requirements and limits: it needs the Go
toolchain and the servicetalk source at build time (located automatically, or
via `--src DIR`) and rebuilds per app; it is **pure-ServiceTalk only** —
foreign (C/Rust) classes need on-disk compilation, so bundle those with
`bundle.sh`. For multi-platform output, also use `bundle.sh`.

### `bundle.sh` — cross-compiled multi-target directory

`bundle.sh` produces a self-contained directory that you can copy to any
host or container that matches one of the targets you cross-compiled
for — no Go toolchain required at the destination.

```bash
./bundle.sh path/to/your/app dist/yourapp
ls dist/yourapp
# Dockerfile  Main.smt  Counter.smt  CCounter/  bin/  run
```

The bundle layout:

```
dist/yourapp/
  Main.smt                # your class files (*.smt, *.st)
  Counter.smt
  CCounter/               # foreign-class dirs travel as-is
  bin/
    smt-linux-amd64       # one binary per cross-compiled target
    smt-linux-arm64
    smt-darwin-arm64
    smt-darwin-amd64
    smt-windows-amd64.exe
    smt-windows-arm64.exe
  run                     # Linux / macOS launcher (POSIX sh)
  run.cmd                 # Windows launcher (cmd.exe)
  Dockerfile              # alpine wrapper, ENTRYPOINT=/app/run
```

`./run` (or `run.cmd` on Windows) accepts the same arguments as `smt`.
It iterates the binaries in `bin/` and exec's the first one that
responds successfully to the `help` subcommand — wrong-architecture
and wrong-OS binaries fail the probe at the kernel `exec()` boundary,
so the right binary wins automatically.  No `uname` mapping, no
brittle `aarch64` / `arm64` aliases.

The smt binary itself auto-discovers its own location (see *Running
the demo*), so the launcher does not `cd` and the user's working
directory is preserved.

```bash
./dist/yourapp/run                                     # default coordinator + REPL
./dist/yourapp/run coordinator --main "" --port 8000   # daemon
./dist/yourapp/run worker --coordinator http://10.0.0.1:8000
.\dist\yourapp\run.cmd coordinator --main ""          REM Windows
```

### Choosing targets

The default target list is `linux/amd64 linux/arm64 darwin/arm64
darwin/amd64 windows/amd64 windows/arm64`.  Override via the `TARGETS`
environment variable:

```bash
TARGETS="linux/amd64" ./bundle.sh app dist/app                       # one target
TARGETS="linux/amd64 linux/arm64" ./bundle.sh app dist/app           # cluster, mixed arch
```

A six-target bundle weighs roughly 40 MB; if you don't need a target
just leave it out.

### macOS `.app` bundles

The bundle layout is close enough to a macOS `.app` that you can
hand-construct one for double-click launch:

```
YourApp.app/
  Contents/
    Info.plist             # standard CFBundleExecutable etc.
    MacOS/
      YourApp              # rename smt-darwin-arm64 (or amd64)
    Resources/
      Main.smt             # class files go here
      Counter.smt
      CCounter/
```

The runtime auto-includes `Contents/Resources` in its classpath, so
the double-clicked app finds its classes even though Finder launches
it with CWD=`/`.

### Containers

The generated `Dockerfile` wraps the bundle into an alpine image with
`gcc` + `musl-dev` preinstalled.  As part of the image build it runs
`/app/run prebuild`, which compiles every foreign C class for the
image's target arch — so first-spawn doesn't pay the `$CC` cost.

```bash
docker build -t yourapp dist/yourapp
docker run --rm -p 8000:8000 yourapp coordinator --main ""
```

For multi-architecture images, `buildx` builds one image per platform
and the `prebuild` step inside each one produces the right per-arch
foreign binary:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
    -t yourapp dist/yourapp --push
```

If your app uses no foreign C classes, drop both the `apk add` and
`/app/run prebuild` lines from the Dockerfile for a smaller image.

### Foreign C classes in the bundle

Foreign-class directories are copied verbatim, including their `.c`
sources.  By default the class binary is built via `$CC` on the host
that first spawns the class and cached alongside the sources.

To avoid that first-spawn cost — or to ship to hosts that have no
toolchain — use `smt prebuild`:

```bash
./run prebuild                       # build every foreign class for the current arch
./run prebuild --path /extra/classes # include an external classpath dir
```

`prebuild` is idempotent (skips classes whose binary is up-to-date by
mtime), so it's safe to wire in early — as a `RUN` in the bundle's
Dockerfile (already done by `bundle.sh`), an init container in
Kubernetes, or a one-shot bake step on each target host.

For cross-arch pre-build outside Docker, drive `prebuild` from inside a
container of the matching platform:

```bash
# Build per-arch foreign binaries by bind-mounting the bundle into a
# matching-platform container and running prebuild there.
for plat in linux/amd64 linux/arm64; do
    docker run --rm --platform="$plat" -v "$(pwd)/dist/yourapp:/app" \
        alpine:3.19 sh -c "apk add --no-cache gcc musl-dev && /app/run prebuild"
done
```

The bundle directory must be writable while `prebuild` runs (or while
a first-spawn compile happens).  In containers this is automatic; on
read-only deployment mounts, run `prebuild` ahead of time and ship
the resulting binaries inside the bundle.

---

## Kubernetes

The end-to-end path is two scripts:

```bash
./bundle.sh    path/to/myapp  dist/myapp                       # 1. build a bundle
./k8s/bundle.sh dist/myapp    registry.example/myapp:v1        # 2. build the image and emit a manifest
docker push                   registry.example/myapp:v1        #    push (skip if --push was used)
kubectl apply -f              dist/myapp/kubernetes.yaml       # 3. roll it out
kubectl port-forward svc/myapp-coordinator 8000:8000           # 4. open the REPL
```

`k8s/bundle.sh BUNDLE_DIR IMAGE_REF [options]` generates a tailored
`kubernetes.yaml` next to your bundle.  The image ref drives both the
`docker build` it runs and the `image:` field in the manifest.  By
default it builds the image for the current host's architecture only;
pass `--platforms linux/amd64,linux/arm64 --push` to produce a
multi-arch image with `buildx` (the bundle already contains the
binaries needed for both).

### Common options

| Flag                  | Meaning                                                   |
|-----------------------|-----------------------------------------------------------|
| `--workers N`         | worker replica count (default 3)                          |
| `--namespace NS`      | target namespace                                          |
| `--name NAME`         | resource-name prefix (default: derived from `IMAGE_REF`)  |
| `--platforms LIST`    | buildx multi-arch platforms (requires `--push`)           |
| `--push`              | push the image after build                                |
| `--apply`             | run `kubectl apply -f` once the manifest is written       |
| `--type TYPE`         | coordinator Service type: `ClusterIP`, `NodePort`, `LoadBalancer` |
| `--node-port PORT`    | the nodePort when `--type=NodePort` (default 30080)       |
| `--no-build`          | skip the docker build (use an image you've already built) |
| `--out FILE`          | manifest output path (default: `BUNDLE_DIR/kubernetes.yaml`) |

`./k8s/bundle.sh --help` prints the full list.

### What the manifest sets up

Reproduced (with placeholders filled in) into the bundle as
`kubernetes.yaml`:

- **Coordinator Deployment**, `replicas: 1`, `strategy: Recreate`.
  The registry is in-memory; rolling updates would split-brain it.
  Liveness and readiness probes hit `/registry`.
- **Coordinator Service** (default `ClusterIP`) named
  `<name>-coordinator`.  Workers and in-cluster clients always address
  it through that DNS name, so the coordinator pod's restart doesn't
  break references.
- **Worker Deployment**, `replicas: N`.  Each worker pod advertises
  `SMT_HOST = status.podIP` via the downward API, so the coordinator
  can reach back to it for spawn requests.  Object method calls go
  through NATS subjects, not worker pod object ports.

### Scaling, accessing, tearing down

```bash
kubectl scale deployment myapp-worker --replicas=10
kubectl get pods -l app=myapp
kubectl logs -l app=myapp,role=coordinator -f
kubectl port-forward svc/myapp-coordinator 8000:8000
kubectl delete -f dist/myapp/kubernetes.yaml
```

Re-running `k8s/bundle.sh` overwrites the manifest in place; apply
again to roll out.  For exposing the web REPL outside the cluster,
pass `--type LoadBalancer` (cloud) or layer an Ingress on top of the
default `ClusterIP` Service.

### Reference

`k8s/cluster.yaml` is the reference shape of what the bundler emits —
a hand-edit starting point if you want to fork it (for sidecars,
custom probes, resource limits, NetworkPolicies, etc.) instead of
running the bundler.

---

## Distributed clustering

Objects can be spawned across a cluster of machines. The coordinator remains the
single leader; **worker** nodes join the cluster and run object processes locally
on their machines.

### Architecture

```
                    ┌─────────────────┐
                    │   Coordinator   │
                    │  (registry,     │
                    │   web REPL,     │
                    │   spawn routing)│
                    └──┬──────────┬───┘
                       │          │
              register/│          │register/
              heartbeat│          │heartbeat
                       │          │
                ┌──────▼──┐  ┌───▼──────┐
                │ Worker 1 │  │ Worker 2 │
                │ (spawns  │  │ (spawns  │
                │  objects │  │  objects │
                │  locally)│  │  locally)│
                └──────────┘  └──────────┘
```

- **Coordinator**: owns the registry, routes spawn requests to workers, serves
  the web REPL.  When no workers are connected it spawns locally (single-node mode).
- **Workers**: register with the coordinator, accept spawn requests over HTTP,
  run `exec.Command` on their local machine, and report back.
- **Objects**: unchanged — they register directly with the coordinator and
  communicate peer-to-peer via HTTP regardless of which machine they live on.

### Quick start — single machine

No workers needed.  Everything works exactly as before:

```bash
go build -o smt .
./smt coordinator --main ""
# open http://localhost:8000/
```

### Quick start — multi-machine cluster

Build a bundle with the architectures of your fleet, then `scp` (or
otherwise copy) the bundle to each machine.  The same bundle runs on
the coordinator host and on every worker — the `run` launcher picks
the right binary at startup.

```bash
./bundle.sh path/to/your/app dist/yourapp
TARGETS="linux/amd64 linux/arm64" ./bundle.sh path/to/your/app dist/yourapp
```

On the **coordinator machine** (e.g. `10.0.0.1`):

```bash
export SMT_HOST=10.0.0.1
./dist/yourapp/run coordinator --port 8000 --main ""
```

On **worker machine A** (e.g. `10.0.0.2`):

```bash
export SMT_HOST=10.0.0.2
./dist/yourapp/run worker --coordinator http://10.0.0.1:8000 --port 8001
```

On **worker machine B** (e.g. `10.0.0.3`):

```bash
export SMT_HOST=10.0.0.3
./dist/yourapp/run worker --coordinator http://10.0.0.1:8000 --port 8001
```

If you'd rather skip the bundle and run directly from a checkout
(e.g. development), substitute `go build -o smt . && ./smt …` for
each `./run …` invocation, and pass `--path /path/to/classes` (or set
`SMT_PATH`) when class files live outside the working directory.

Objects created via `(new Counter)` in the REPL or from `.smt` code will be
randomly placed on any available worker.

### Quick start — Docker cluster

A `Dockerfile` (multi-stage Go build → alpine runtime) and a
`docker-compose.yml` are included for the local cluster smoke test:

```bash
docker compose up
# open http://localhost:8000/
```

This starts a coordinator and two workers on a shared Docker network.
Objects spawn across both workers.

For real deployments, prefer the bundle path described in *Packaging
for deployment*: it produces a smaller image, supports multi-arch, and
keeps the dev/prod artifact consistent across hosts.

```bash
docker compose logs -f     # watch where objects land
docker compose stop worker1  # test failover
docker compose up -d worker1 # bring it back
docker compose down          # tear down everything
```

### Command reference

```
smt coordinator [flags]
    --port PORT        Coordinator listen port (default 8000)
    --main CLASS       Class to spawn on startup (default "Main", empty to skip)
    --path DIR         Extra directory to search for class files (repeatable)
    --registry-file F  Persist the registry to F and reload it on restart
                       (also via SMT_REGISTRY_FILE; off by default)
    --pulse-interval MS  Pulse-GC sweep interval in ms
                       (0 = use SMT_PULSE_INTERVAL_MS or the 10000 default)
    --verbose          Print lifecycle messages to stderr (default true)

smt worker [flags]
    --coordinator URL  Coordinator URL (default http://localhost:8000)
    --port PORT        Worker listen port (default 8001)
    --host HOST        Advertised hostname (auto-detected if empty)
    --path DIR         Extra directory to search for class files (repeatable)

smt object [flags]       (internal — used by coordinator/worker to spawn processes)
    --class CLASS      Class name
    --id ID            Object UUID
    --coordinator URL  Coordinator URL
    --init             Call initialize on startup
    --def FILE         Block definition JSON file
    --pulse-timeout SECS  Shut down if no pulse received this long (default 60, 0 disables)
    --path DIR         Extra directory to search for class files (repeatable)
    --verbose          Print lifecycle messages to stderr (children inherit via SMT_VERBOSE)

smt prebuild [flags]
    --path DIR         Extra directory to search for class files (repeatable)

smt run DIR [flags]
    --main CLASS       Entry class to spawn (default "Main", empty to skip)
    --port PORT        Coordinator listen port (default 8000)
    --eval EXPR        Evaluate against the app, print, and exit
    --path DIR         Additional class directory (repeatable)

smt build DIR [flags]   (needs the Go toolchain + servicetalk source)
    -o BIN             Output binary path (default ./<DIR-name>)
    --main CLASS       Class the built binary auto-spawns (default "Main")
    --src DIR          servicetalk source directory (default: auto-detect)
```

The classpath order is: CWD, the smt binary's directory, that
directory's parent, the parent's `Resources/`, then `$SMT_PATH`
(PATH-style), then any `--path` flags in the order given.  Children
spawned by coordinator or worker inherit the user-supplied entries
(`SMT_PATH` + `--path`) unchanged; the binary-relative entries are
recomputed in the child from its own `os.Executable()`.

### Hostname resolution

Each process needs to know its externally-reachable address.  Resolution order:

1. **`SMT_HOST` environment variable** — set this explicitly in cloud/container
   environments where auto-detection may pick the wrong interface.
2. **Auto-detect** — scans network interfaces for the first non-loopback IPv4
   address.
3. **Fallback** — `"localhost"` (single-node mode).

In Docker Compose, set `SMT_HOST` to the container's service name
(e.g. `worker1`) since Docker DNS resolves service names within the network.

### Spawn routing

When the coordinator receives a `/spawn` request:

1. If **no workers** are registered → spawn locally via `exec.Command` (backward
   compatible with the original single-node design).
2. If **workers exist** → pick one at random and forward the `SpawnRequest`.
   The worker runs `exec.Command` on its machine, waits for the object to
   register with the coordinator, and returns the response.

All objects register directly with the coordinator regardless of which machine
spawned them.  The registry stores actual hostnames, so objects on different
machines can call each other directly.

### Block spawning across machines

Blocks are serialized inline in the spawn request (not via temp files on a
shared filesystem).  The spawning machine (coordinator or worker) writes the
block definition to a local temp file and passes it to the block process via
`--def`.  The block process reads and deletes the file on startup, just as in
single-node mode.

### Worker lifecycle

- Workers send a heartbeat to the coordinator every 5 seconds.
  Workers are coarse-grained (a few per cluster), so the heartbeat
  cost is negligible — the per-object heartbeat was the
  problematic one and is gone (see *Object lifecycle* above).
- The coordinator evicts workers silent for >30 seconds.
- When a worker dies, its objects fail to receive pulses and
  self-deregister via the pulse-timeout mechanism.
- New workers can join at any time — the coordinator starts routing spawns to
  them immediately.

### Class file deployment

When the coordinator delegates a spawn to a worker, it sends a digest-keyed
source bundle built from the current classpath.  The worker caches that
bundle under its user cache directory and puts it first on the spawned
object's classpath.  `.smt`, `.st`, and foreign-class directories are
included; generated foreign build products and Cargo `target/` trees are
not.  Foreign classes compile on first spawn on the worker (worker needs
`cc`/Cargo reachable if the class uses those toolchains; the bundle's
Dockerfile installs the C toolchain).

For ad-hoc setups without a bundle:

- **Shared mount**: mount a directory containing the classes on every
  host and pass `--path` (or set `SMT_PATH`) to point at it.
- **Replicated copy**: rsync or scp the class directory to each
  machine — useful when you want to avoid sending source bundles at spawn
  time.

If you'd rather pre-compile foreign classes for a given architecture,
build them once on a matching host and commit/ship the resulting
binary alongside the `.c` files.  The build step becomes a no-op when
mtimes say it's up to date.

### Monitoring

The coordinator logs all registration, eviction, and worker events to stdout:

```
[coordinator] worker registered: a1b2c3d4 at worker1:8001
[coordinator] registered f7e41e47 (Counter) at smt.default.obj.f7e41e47.call
[coordinator] evicting dead worker a1b2c3d4 (worker1:8001)
[f7e41e47] no pulse for 1m0s, shutting down
```

The **Live Objects** panel in the web REPL shows the subject for each object,
making it easy to see which worker is running what.

The coordinator's `/metrics` endpoint returns runtime counters, spawn and
pulse latency totals/maxes, and best-effort per-object mailbox depths:

```fish
curl -s http://localhost:8000/metrics | jq .
```

The coordinator's `/registry` endpoint returns the full registry as JSON:

```fish
curl -s http://localhost:8000/registry | jq .
```

### Failure modes

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Worker crash | Worker heartbeat timeout (30s) | Coordinator removes worker from pool; its objects fail their pulse-timeout (60s default) and self-deregister |
| Object crash | Coordinator's stale-prune sweep finds the unresponsive entry and evicts it (1 ping per `SMT_STALE_PRUNE_INTERVAL_MS`) | Registry entry removed; survivors stop forwarding via pulse-timeout |
| Coordinator crash | Everything stops | Restart coordinator; workers and objects re-register on next attempted action |
| Network partition | Pulse delivery failures, registry-fetch failures | Affected objects miss pulses, self-deregister after timeout; recover when network heals |

### Example: observing distribution

```smalltalk
"In the REPL — create several objects"
c1 := Counter new.
c2 := Counter new.
c3 := Counter new.

"Check the registry to see hosts"
coordinator printRegistry.
"Output shows objects on different workers:
   Counter  f7e41e47  worker1:10001
   Counter  29ee3239  worker2:10000
   Counter  5b92669c  worker1:10002"

"Cross-node calls work transparently"
c1 increment.
c2 increment.
c1 value.   "→ 1"
c2 value.   "→ 1"

"Blocks work across nodes too"
arr := Array new.
arr add: 10.
arr add: 20.
arr do: [x | Transcript show: x printString ].   "block may run on a different worker"
"Output: 10  20"
```
