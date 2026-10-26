; Shell.smt — native servicetalk shell.
;
; The "shell" here is a Smalltalk object whose methods ARE the shell
; commands.  In `smt shell`, every line you type is parsed as Smalltalk
; and evaluated with `self` bound to a Shell instance, so unary names
; like `ls` and `pwd` self-dispatch and keyword forms like `cd: '/tmp'`
; are bare-keyword self-sends.
;
; Examples (from the shell prompt):
;
;   ls                            "Array of names in current dir"
;   pwd                           "current directory string"
;   cd: '/tmp'                    "change cwd, return new cwd"
;   home                          "$HOME"
;   getenv: 'PATH'                "OS env var"
;   exec: 'sleep 60'              "spawn a Process, return it"
;   run: 'whoami'                 "exec + wait + output  (sync)"
;   cat: 'README.md'              "read file as string"
;   jobs                          "Array of every Process in the cluster"
;
; Composing is normal Smalltalk:
;
;   (self exec: 'sleep 1') wait.   (self exec: 'sleep 1') output.
;   self ls do: [:n | Transcript show: n , Char lf].
;
; Process-spawning has no shell wrapping by default — the line you
; pass to `exec:` / `run:` is `sh -c`'d with a `cd <Shell.cwd> &&`
; prefix so cwd applies, but no env-var injection or alias rewriting.
; If you want env, `(self exec: 'FOO=bar some-cmd args')` does it.

(class Shell
  (vars cwd
        ; transient state declared per the strict-vars rule
        p out path)

  (method initialize ()
    (set cwd (send "." absolutePath)))

  ; ---- accessors ----

  (method cwd () cwd)
  (method pwd () cwd)
  (method home () (send "HOME" getenv))

  ; ---- directory navigation ----

  ; `ls` / `ls: 'somedir'` return an Array of names.  The REPL prints
  ; the Array using its normal `#(...)` printString, so the result stays
  ; composable instead of being pre-flattened into a display string.
  (method ls ()
    (send cwd dirEntries))

  (method ls: (path)
    (send (resolvePath: path) dirEntries))

  (method cd: (path)
    (set path (resolvePath: path))
    (if (send path isDirectory)
      (set cwd path)
      (return (concat "cd: not a directory: " path)))
    cwd)

  ; ---- environment ----

  (method getenv: (name)
    (send name getenv))

  ; ---- file IO ----

  (method cat: (path)
    (set path (resolvePath: path))
    (if (send path fileExists)
      (send path readText)
      (concat "cat: no such file: " path)))

  ; ---- subprocesses ----

  ; `exec: 'cmd args'` — start a Process via `sh -c <line>` with the
  ; Shell's cwd applied as the spawned process's working directory
  ; (no `cd '…' &&` prefix in the stored command — the user sees back
  ; what they typed).  Doesn't block, doesn't capture output — that's
  ; what `wait` / `output` are for on the returned Process.
  (method exec: (line)
    (set p (new Process))
    (send p start: line cwd: cwd)
    p)

  ; `run: 'cmd args'` — shorthand for exec + wait + output, the common
  ; "shell command" case.  Blocks until the process exits, returns the
  ; captured stdout+stderr.  For long-running things use `exec:` and
  ; manage the Process yourself.
  (method run: (line)
    (set p (exec: line))
    (send p wait)
    (send p output))

  ; ---- jobs ----

  ; Every Process in the cluster.  Returns the Array as-is — each
  ; Process has a compact printString (status / pid / command line).
  ; Smalltalk-pure: the result IS a list of Processes.
  (method jobs ()
    (send coordinator objectsOfClass: "Process"))

  ; ---- help ----

  (method help ()
    (concat
      "smt shell — methods on `self` (Shell):\n"
      "  navigation\n"
      "    pwd / cwd                    current directory (string)\n"
      "    home                         $HOME\n"
      "    ls                           directory listing as an Array\n"
      "    ls: 'somedir'                listing of 'somedir' as an Array\n"
      "    cd: '/tmp'                   change cwd, return new cwd\n"
      "  files\n"
      "    cat: 'README.md'             read file as string\n"
      "  subprocesses\n"
      "    exec: 'sleep 60'             spawn a Process, return it\n"
      "    run: 'whoami'                exec + wait + output (sync)\n"
      "    jobs                         every Process in cluster as an Array\n"
      "  environment\n"
      "    getenv: 'PATH'               OS env var, or nil\n"
      "  meta\n"
      "    help                         this card\n"
      "    exit / :quit / Ctrl-D        leave\n"
      "\n"
      "Everything else is plain Smalltalk:\n"
      "  Counter new           coordinator allObjects size\n"
      "  c := Counter new      c increment. c value\n"
      "  cwd dirEntries        the Array form of `ls`\n"
      "  coordinator objectsOfClass: 'Process'      Array form of `jobs`\n"
      "\n"
      "Helpful built-ins on common types:\n"
      "  String : size at: , (concat) trimSeparators startsWith: endsWith:\n"
      "           includes: replaceAll:with: indexOf: copyFrom:to:\n"
      "           dirEntries fileExists isDirectory readText\n"
      "           absolutePath joinPath: getenv splitOn:\n"
      "  Number : + - * / = < > printString to:do: timesRepeat:\n"
      "  Array  : size at: at:put: add: do: collect: first last printString\n"
      "  Dictionary : at: at:put: keys values keysAndValuesDo: printString\n"))

  ; ---- helpers ----

  ; resolvePath: turn a user-supplied path into an absolute one against
  ; the Shell's cwd.  Handles `/abs`, `~` (home), `~/relative`, and
  ; plain relative paths.
  (method resolvePath: (path)
    (if (send path = "~") (return (home)))
    (if (send path startsWith: "/") (return path))
    (if (send path startsWith: "~/")
      (return (send (send (home) joinPath: (send path copyFrom: 2 to: (send path size))) absolutePath)))
    (send (send cwd joinPath: path) absolutePath))

  ; shQuote: wrap s in single quotes for safe sh inclusion; escape any
  ; embedded single quote as '\''.
  (method shQuote: (s)
    (concat "'" (send s replaceAll: "'" with: "'\\''") "'"))

  (method doesNotUnderstand: (msg)
    (concat "Shell: I don't understand " msg)))
