package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

// coordinatorID is the fixed Smalltalk object ID for the coordinator itself.
// The symbol "coordinator" in .smt code resolves to ObjVal(coordinatorID).
const coordinatorID = "coordinator"

type Coordinator struct {
	mu           sync.RWMutex
	registry     map[string]*ObjectEntry
	spawnWaiters sync.Map // objectID -> chan struct{}
	myURL        string

	// workspace: the persistent REPL object
	workspaceID string
	workspaceMu sync.Mutex

	// cluster workers
	workers  map[string]*WorkerEntry
	workerMu sync.RWMutex

	// locally-spawned child objects (for --eval cleanup)
	childrenMu sync.Mutex
	children   map[string]*localChild

	// Pulse-GC roots.  The coordinator is the single authority on what
	// counts as live: every pulseInterval it mints a fresh sweep id and
	// sends pulse: to every root, which propagates through pulse: to
	// transitively reachable objects.  Anything no root reaches dies on
	// its pulseTimeout.
	rootsMu       sync.Mutex
	roots         map[string]struct{}
	pulseInterval time.Duration

	// Optional registry persistence.  When registryFile is non-empty the
	// coordinator snapshots the registry (and pulse roots) to disk shortly
	// after anything changes, and reloads + revalidates it on startup.  A
	// coordinator restart can then re-adopt objects that outlived it
	// (objects on separate machines, or a coordinator crash that didn't
	// take the children down) instead of forcing a whole-cluster teardown.
	// Empty (the default) keeps the original in-memory-only behaviour.
	registryFile  string
	registryDirty atomic.Bool

	// Slow stale-prune sweep.  Objects that hard-crash before they
	// can `/deregister` leak into the registry; pulse-GC doesn't
	// notice because it only walks live references.  This sweep
	// pings one object per stalePruneInterval and evicts any that
	// don't answer.  At the default 1s interval and 20K objects, a
	// full pass takes ~5.5 hours — slow enough not to dent the
	// coordinator's CPU, fast enough that long-running clusters
	// don't fill up with dead entries.  Tune via
	// SMT_STALE_PRUNE_INTERVAL_MS.
	stalePruneInterval time.Duration
	staleFailures map[string]time.Time

	metrics coordinatorCounterSet
	trace traceBuffer
	traceEnabled atomic.Bool
	sources sourceStore
}

// Mode constants for runCoordinator.  "serve" blocks forever (daemon / web
// REPL), "eval" runs a single expression and exits, "repl" drops into an
// interactive terminal REPL.
const (
	ModeServe = "serve"
	ModeEval  = "eval"
	ModeScript = "script"
	ModeRepl  = "repl"
	ModeShell = "shell"
)

func runCoordinator(port int, mainClass string, mode string, evalExpr string) {
	address := fmt.Sprintf(":%d", port)
	if mode == ModeScript {
		address = fmt.Sprintf("127.0.0.1:%d", port)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fatalf("coordinator: listen on port %d: %v", port, err)
	} else {
		port = listener.Addr().(*net.TCPAddr).Port
	}
	defer listener.Close()
	coordinatorURL := selfURL(port)
	if mode == ModeScript {
		coordinatorURL = "http://" + listener.Addr().String()
	}
	c := &Coordinator{
		registry:           make(map[string]*ObjectEntry),
		myURL:              coordinatorURL,
		workers:            make(map[string]*WorkerEntry),
		children:           make(map[string]*localChild),
		roots:              make(map[string]struct{}),
		pulseInterval:      envDuration("SMT_PULSE_INTERVAL_MS", 10*time.Second, time.Millisecond),
		stalePruneInterval: envDuration("SMT_STALE_PRUNE_INTERVAL_MS", 1*time.Second, time.Millisecond),
		registryFile:       os.Getenv("SMT_REGISTRY_FILE"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/register", c.handleRegister)
	mux.HandleFunc("/deregister", c.handleDeregister)
	mux.HandleFunc("/spawn", c.handleSpawn)
	mux.HandleFunc("/registry", c.handleRegistry)
	mux.HandleFunc("/navigation", c.handleNavigation)
	mux.HandleFunc("/relationships", c.handleRelationships)
	mux.HandleFunc("/navigation.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, navigationJS) })
	mux.HandleFunc("/metrics", c.handleMetrics)
	mux.HandleFunc("/mcp", c.handleMCP)
	mux.HandleFunc("/transcript", c.handleTranscript)
	mux.HandleFunc("/message", c.handleMessage) // coordinator as Smalltalk object
	mux.HandleFunc("/eval", c.handleEval)
	mux.HandleFunc("/workspace", c.handleObjectWorkspace)
	mux.HandleFunc("/interaction", c.handleInteraction)
	mux.HandleFunc("/result", c.handleResult)
	mux.HandleFunc("/recording", c.handleRecording)
	mux.HandleFunc("/view", c.handleInspectorView)
	mux.HandleFunc("/debugger", c.handleDebugger)
	mux.HandleFunc("/trace", c.handleTrace)
	mux.HandleFunc("/source", c.handleSource)
	mux.HandleFunc("/wiring", c.handleWiring)
	mux.HandleFunc("/workbench", c.handleWorkbench)
	mux.HandleFunc("/refactor", c.handleRefactor)
	mux.HandleFunc("/refactoring.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, refactoringJS) })
	mux.HandleFunc("/examples", c.handleExamples)
	mux.HandleFunc("/workbench.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, workbenchJS) })
	mux.HandleFunc("/visual-editor.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, visualEditorJS) })
	mux.HandleFunc("/promises.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, promisesJS) })
	mux.HandleFunc("/wiring.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, wiringJS) })
	mux.HandleFunc("/cloning.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, cloningJS) })
	mux.HandleFunc("/source-history.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, sourceHistoryJS) })
	mux.HandleFunc("/debugger.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, debuggerJS) })
	mux.HandleFunc("/trace.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, traceJS) })
	mux.HandleFunc("/interaction.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, interactionJS) })
	mux.HandleFunc("/views.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, inspectorViewsJS) })
	mux.HandleFunc("/object-workspace.js", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/javascript"); fmt.Fprint(w, objectWorkspaceJS) })
	mux.HandleFunc("/inspect", c.handleSelfInspect) // coordinator self-inspection
	mux.HandleFunc("/object/", c.handleObjectDebug)
	mux.HandleFunc("/spawn-failed", c.handleSpawnFailed)
	mux.HandleFunc("/worker-heartbeat", c.handleWorkerHeartbeat)
	mux.HandleFunc("/", c.handleUI) // web REPL — catch-all serves UI
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // coordinator doesn't initiate messages, ignore replies
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// /shutdown — graceful coordinator exit.  Asks spawned object
	// subprocesses to shut down, then exits.  Direct HTTP endpoint (rather than
	// `coordinator shutdown` via /eval) so the response can flush before
	// the workspace child dies.  Application's own exit endpoint — not
	// a shell kill from outside.
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "coordinator: shutting down")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			c.shutdownLocalChildren(5 * time.Second)
			time.Sleep(50 * time.Millisecond)
			os.Exit(0)
		}()
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[coordinator] server error: %v\n", err)
			os.Exit(1)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	if err := c.startNATSReceiver(); err != nil {
		fmt.Fprintf(os.Stderr, "[coordinator] NATS receiver: %v\n", err)
		os.Exit(1)
	}

	logInfo("[coordinator] listening on port %d, object transport %s\n", port, natsURL())

	// Register coordinator itself as a Smalltalk object with a fixed ID.
	// Objects can send messages to it using the symbol 'coordinator'.
	c.mu.Lock()
	c.registry[coordinatorID] = &ObjectEntry{
		ID:        coordinatorID,
		Subject:   natsSubjectForObject(coordinatorID),
		ClassName: BuiltinClassCoordinator,
		LastSeen:  time.Now(), // diagnostic — registration time, not heartbeat-driven
	}
	c.mu.Unlock()

	// Reload a persisted registry (if configured) before the loops start,
	// so revalidated objects are present for the first pulse sweep.
	if c.registryFile != "" {
		c.loadRegistry()
		go c.persistLoop()
	}

	go c.workerEvictionLoop()
	go c.pulseLoop()
	go c.stalePruneLoop()

	if mainClass != "" {
		go func() {
			logInfo("[coordinator] spawning %s --init\n", mainClass)
			resp, err := c.spawnObject(mainClass, nil, true, 60)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[coordinator] failed to spawn %s: %v\n", mainClass, err)
				return
			}
			logInfo("[coordinator] %s started as %s at %s\n", mainClass, resp.ObjectID, resp.Subject)
			c.addRoot(resp.ObjectID)
		}()
	}

	switch mode {
	case ModeEval, ModeScript:
		var code int
		if mode == ModeScript {
			code = c.runOneShotEvalMethod(evalExpr, "evalScript:")
		} else {
			code = c.runOneShotEval(evalExpr)
		}
		c.shutdownLocalChildren(5 * time.Second)
		os.Exit(code)
	case ModeRepl:
		c.runRepl()
		c.shutdownLocalChildren(5 * time.Second)
		os.Exit(0)
	case ModeShell:
		c.runShell()
		c.shutdownLocalChildren(5 * time.Second)
		os.Exit(0)
	default:
		select {} // block forever (web REPL / daemon)
	}
}

// evalOnWorkspace sends expr to the workspace object's eval: method and
// returns the result string.  Spawns the workspace if it isn't running yet.
func (c *Coordinator) evalOnWorkspace(expr string) (string, error) {
	return c.evalOnWorkspaceMethod(expr, "eval:")
}

func (c *Coordinator) evalOnWorkspaceMethod(expr, method string) (string, error) {
	wsID, err := c.ensureWorkspace()
	if err != nil {
		return "", fmt.Errorf("ensureWorkspace: %w", err)
	}
	c.mu.RLock()
	entry, ok := c.registry[wsID]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("workspace %s not in registry", wsID)
	}
	callResp, err := c.callObject(entry, method, []Value{StrVal(expr)}, evalTimeout())
	if err != nil {
		return "", fmt.Errorf("workspace %s unreachable at %s: %w", wsID, entry.Subject, err)
	}
	return callResp.Str, nil
}

func (c *Coordinator) callObject(entry *ObjectEntry, method string, args []Value, timeout time.Duration) (Value, error) {
	reply, err := sendNATSCall(entry.Subject, method, args, timeout)
	if err != nil {
		return Nil, err
	}
	if reply.Signaled {
		return Nil, fmt.Errorf("remote signal: %s", reply.SignalMsg)
	}
	return reply.Result, nil
}

// runOneShotEval evaluates expr and prints the result.  Used by --eval.
func (c *Coordinator) runOneShotEval(expr string) int {
	return c.runOneShotEvalMethod(expr, "eval:")
}

func (c *Coordinator) runOneShotEvalMethod(expr, method string) int {
	result, err := c.evalOnWorkspaceMethod(expr, method)
	if err != nil {
		fmt.Fprintf(os.Stderr, "eval: %v\n", err)
		return 1
	} else {
		fmt.Println(result)
		return 0
	}
}

// runRepl reads Smalltalk input, evaluates it against the workspace,
// and prints the result.  Exits cleanly on EOF (Ctrl+D) or :quit.
func (c *Coordinator) runRepl() {
	if _, err := c.ensureWorkspace(); err != nil {
		fmt.Fprintf(os.Stderr, "repl: cannot start workspace: %v\n", err)
		return
	}
	fmt.Println()
	fmt.Println("smt REPL — Smalltalk syntax.  Type `help` for a starter card.")
	fmt.Println("           For shell commands (ls, cat, run, jobs…) use `smt shell` instead.")
	fmt.Println("           Ctrl+D or :quit to exit.  Web REPL also at", c.myURL)
	fmt.Println()

	stdinFd := int(os.Stdin.Fd())
	if term.IsTerminal(stdinFd) {
		c.runReplTerminal(stdinFd)
		return
	}
	c.runReplPlain()
}

func (c *Coordinator) runReplTerminal(stdinFd int) {
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repl: cannot put terminal in raw mode — %v\n", err)
		c.runReplPlain()
		return
	}
	defer term.Restore(stdinFd, oldState)

	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "> ")
	for {
		full, ok := replReadTerminalInput(t)
		if !ok {
			fmt.Fprintln(t)
			return
		}
		if c.handleReplInput(full, t) {
			return
		}
	}
}

func replReadTerminalInput(t *term.Terminal) (string, bool) {
	line, err := t.ReadLine()
	if err == io.EOF {
		return "", false
	}
	if err != nil {
		fmt.Fprintf(t, "repl: read: %v\r\n", err)
		return "", false
	}
	full := line
	for !smalltalkComplete(full) {
		t.SetPrompt("... > ")
		line, err := t.ReadLine()
		t.SetPrompt("> ")
		if err == io.EOF {
			return "", false
		}
		if err != nil {
			fmt.Fprintf(t, "repl: read: %v\r\n", err)
			return "", false
		}
		full += "\n" + line
	}
	return full, true
}

func (c *Coordinator) runReplPlain() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			fmt.Println()
			return
		}
		full := sc.Text()
		// Gather continuation lines until brackets/parens/braces balance,
		// so multi-line class definitions and blocks can be pasted or typed
		// directly.  The "... > " prompt signals we're inside an open form.
		for !smalltalkComplete(full) {
			fmt.Print("... > ")
			if !sc.Scan() {
				fmt.Println()
				return
			}
			full += "\n" + sc.Text()
		}
		if c.handleReplInput(full, os.Stdout) {
			return
		}
	}
}

func (c *Coordinator) handleReplInput(full string, out io.Writer) bool {
	trimmed := strings.TrimSpace(full)
	if trimmed == "" {
		return false
	}
	if trimmed == ":quit" || trimmed == ":q" {
		return true
	}
	if trimmed == "help" || trimmed == ":help" || trimmed == "?" || trimmed == "h" {
		fmt.Fprintln(out, helpText())
		return false
	}
	result, err := c.evalOnWorkspace(full)
	if err != nil {
		fmt.Fprintf(out, "error: %v\n", err)
		return false
	}
	fmt.Fprintln(out, result)
	return false
}

// smalltalkComplete reports whether src is a complete top-level input —
// i.e. every (), [], and {} is closed and we're not mid-string or
// mid-comment.  Used by the terminal REPL to decide whether to keep
// reading continuation lines.  String literals ('…', with ” as an
// escaped quote) and comments ("…") are skipped so brackets inside them
// don't affect the count.  Conservative: an unbalanced *closing* bracket
// reads as complete (depth clamped at 0) so a stray ']' can't wedge the
// prompt into an unending continuation.
func smalltalkComplete(src string) bool {
	depth := 0
	inStr := false
	inComment := false
	runes := []rune(src)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case inComment:
			if c == '"' {
				inComment = false
			}
		case inStr:
			if c == '\'' {
				if i+1 < len(runes) && runes[i+1] == '\'' {
					i++ // doubled '' — an escaped quote, stay in string
					continue
				}
				inStr = false
			}
		default:
			switch c {
			case '\'':
				inStr = true
			case '"':
				inComment = true
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return depth == 0 && !inStr && !inComment
}

// runShell is `smt shell`: a Smalltalk REPL with shell-flavored
// `self` bound to a Shell instance, so unary names like `ls` and
// `pwd` self-dispatch and bare-keyword forms like `cd: '/tmp'` are
// keyword self-sends.  The "shell commands" are Smalltalk methods
// on the Shell class (examples/shell/Shell.smt).
//
// Line editing / history / Tab completion come from golang.org/x/term.
//
// Exits cleanly on EOF (Ctrl-D), `exit`, or `:quit`.
func (c *Coordinator) runShell() {
	resp, err := c.spawnObject("Shell", nil, true, 60)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell: failed to spawn Shell — %v\n", err)
		fmt.Fprintln(os.Stderr, "       (Shell.smt must be in the classpath; pass --path examples/shell or run from a tree that includes it)")
		return
	}
	c.addRoot(resp.ObjectID)
	shellRef := ObjectEntry{ID: resp.ObjectID, Subject: resp.Subject}

	wsID, err := c.ensureWorkspace()
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell: cannot start workspace: %v\n", err)
		return
	}
	c.mu.RLock()
	wsEntry, ok := c.registry[wsID]
	c.mu.RUnlock()
	if !ok {
		fmt.Fprintf(os.Stderr, "shell: workspace not in registry\n")
		return
	}
	if err := setWorkspaceShellContext(*wsEntry, shellRef.ID); err != nil {
		fmt.Fprintf(os.Stderr, "shell: cannot bind shell context: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("smt shell — Smalltalk REPL with `self` bound to a Shell instance.")
	fmt.Println("            Type `ls`, `pwd`, `cd: '/tmp'`, `run: 'whoami'`, `jobs`.")
	fmt.Println("            `Counter new` etc. work too — it's a full Smalltalk REPL.")
	fmt.Println("            Type `help` for the full method list, `exit` / Ctrl-D to leave.")
	fmt.Println()

	stdinFd := int(os.Stdin.Fd())
	if !term.IsTerminal(stdinFd) {
		c.runShellPlain(shellRef)
		return
	}
	oldState, err := term.MakeRaw(stdinFd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell: cannot put terminal in raw mode — %v\n", err)
		c.runShellPlain(shellRef)
		return
	}
	defer term.Restore(stdinFd, oldState)

	t := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}, "")
	t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' {
			return "", 0, false
		}
		return shellComplete(shellRef, line, pos)
	}

	for {
		t.SetPrompt(fmt.Sprintf("%s $ ", filepath.Base(shellCwdHint(shellRef))))
		line, err := t.ReadLine()
		if err == io.EOF {
			fmt.Fprintln(t)
			return
		}
		if err != nil {
			fmt.Fprintf(t, "shell: read: %v\r\n", err)
			return
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if trim == ":quit" || trim == ":q" || trim == "exit" {
			return
		}
		out, err := c.evalOnWorkspace(line)
		if err != nil {
			fmt.Fprintf(t, "shell: %v\r\n", err)
			continue
		}
		if out != "" {
			fmt.Fprint(t, strings.ReplaceAll(out, "\n", "\r\n"))
			if !strings.HasSuffix(out, "\n") {
				fmt.Fprint(t, "\r\n")
			}
		}
	}
}

// runShellPlain is the fallback when stdin isn't a terminal (piped
// input from a script, etc.) — same line dispatch but no raw mode,
// no completion, no history.
func (c *Coordinator) runShellPlain(shellRef ObjectEntry) {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		fmt.Printf("%s $ ", filepath.Base(shellCwdHint(shellRef)))
		if !sc.Scan() {
			fmt.Println()
			return
		}
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if trim == ":quit" || trim == ":q" || trim == "exit" {
			return
		}
		out, err := c.evalOnWorkspace(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "shell: %v\n", err)
			continue
		}
		if out != "" {
			if !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			fmt.Print(out)
		}
	}
}

// setWorkspaceShellContext POSTs setVar:to: to the workspace, binding
// `shellContext` to the Shell objid.  workspace.eval: reads this and
// runs every line with selfOverride set to the Shell.
func setWorkspaceShellContext(ws ObjectEntry, shellObjID string) error {
	reply, err := sendNATSCall(ws.Subject, "setVar:to:", []Value{StrVal("shellContext"), ObjVal(shellObjID)}, 30*time.Second)
	if err != nil {
		return err
	}
	if reply.Signaled {
		return fmt.Errorf("remote signal: %s", reply.SignalMsg)
	}
	return nil
}

// shellComplete — Tab callback.  At verb position (start-of-line)
// completes against Smalltalk identifiers visible in the Shell + a
// few class names; later positions fall through to filesystem
// completion against the Shell's cwd, which is what shell users
// expect when typing args to `run:`, `cat:`, `cd:`, etc.
func shellComplete(shell ObjectEntry, line string, pos int) (string, int, bool) {
	pre := line[:pos]
	if !strings.ContainsAny(pre, " \t'\"") {
		matches := matchPrefix(pre, shellNames())
		return applyMatch(line, pos, pre, matches)
	}
	// Filesystem completion — find the trailing fragment after the
	// last whitespace OR opening quote.
	frag := pre
	for i := len(pre) - 1; i >= 0; i-- {
		c := pre[i]
		if c == ' ' || c == '\t' || c == '\'' || c == '"' {
			frag = pre[i+1:]
			break
		}
	}
	matches := matchPath(shell, frag)
	return applyMatch(line, pos, frag, matches)
}

// shellNames is the list of identifiers Tab offers at verb position.
// Mirrors what the Shell class exposes plus a few common class names
// so `Coun<TAB>` → `Counter `.
func shellNames() []string {
	return []string{
		// Shell methods
		"ls", "pwd", "cd:", "home", "cwd", "help",
		"getenv:", "exec:", "run:", "cat:", "jobs",
		"write:text:", "append:text:", "mkdir:", "find:", "grep:in:",
		"env:", "env:put:", "unsetEnv:", "environment", "lastProcess",
		"exec:args:", "run:args:", "exec:args:input:", "run:args:input:", "checked:args:",
		// Common classes
		"Counter", "Process", "Dictionary", "Array",
		"HttpClient", "HttpServer", "RobotsMatcher", "SitemapParser",
		// Coordinator surface
		"coordinator",
		// Quit
		"exit", ":quit",
	}
}

func matchPrefix(prefix string, cands []string) []string {
	var out []string
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func matchPath(shell ObjectEntry, frag string) []string {
	dir, base := splitPathFrag(frag)
	resolved := resolvePathDir(shell, dir)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if strings.HasPrefix(base, ".") || !strings.HasPrefix(name, ".") {
			full := dir + name
			if e.IsDir() {
				full += "/"
			}
			out = append(out, full)
		}
	}
	return out
}

func splitPathFrag(frag string) (dir, base string) {
	i := strings.LastIndex(frag, "/")
	if i < 0 {
		return "", frag
	}
	return frag[:i+1], frag[i+1:]
}

func resolvePathDir(shell ObjectEntry, dir string) string {
	if dir == "" {
		return shellCwdHint(shell)
	}
	if strings.HasPrefix(dir, "/") {
		return dir
	}
	if strings.HasPrefix(dir, "~/") {
		return filepath.Join(os.Getenv("HOME"), dir[2:])
	}
	if dir == "~" {
		return os.Getenv("HOME")
	}
	return filepath.Join(shellCwdHint(shell), dir)
}

func applyMatch(line string, pos int, frag string, matches []string) (string, int, bool) {
	if len(matches) == 0 {
		return "", 0, false
	}
	best := matches[0]
	if len(matches) > 1 {
		best = longestCommonPrefix(matches)
		if best == frag {
			return "", 0, false
		}
	}
	suffix := best[len(frag):]
	if len(matches) == 1 && !strings.HasSuffix(best, "/") && !strings.HasSuffix(best, ":") {
		suffix += " "
	}
	newLine := line[:pos] + suffix + line[pos:]
	return newLine, pos + len(suffix), true
}

func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// shellCwdHint reads the Shell's `cwd` instance var directly via the
// universal `getVar:` method.  Going through `Shell run: 'pwd'` would
// add a history entry per prompt — noisy and wrong (the user didn't
// type `pwd`).
func shellCwdHint(shell ObjectEntry) string {
	reply, err := sendNATSCall(shell.Subject, "getVar:", []Value{StrVal("cwd")}, 3*time.Second)
	if err != nil {
		return "?"
	}
	if reply.Signaled {
		return "?"
	}
	return reply.Result.Str
}

type localChild struct {
	id   string
	done chan error
}

// shutdownLocalChildren asks every object process spawned locally by this
// coordinator to deregister and exit, then waits briefly for the child
// processes to finish.
func (c *Coordinator) shutdownLocalChildren(timeout time.Duration) {
	c.childrenMu.Lock()
	children := make([]*localChild, 0, len(c.children))
	for _, child := range c.children {
		children = append(children, child)
	}
	c.children = make(map[string]*localChild)
	c.childrenMu.Unlock()
	for _, child := range children {
		_, _ = sendNATSCall(natsSubjectForObject(child.id), "__shutdown", nil, 2*time.Second)
	}
	deadline := time.Now().Add(timeout)
	for _, child := range children {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case <-child.done:
		case <-time.After(remaining):
			return
		}
	}
}

func (c *Coordinator) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad register request: "+err.Error(), http.StatusBadRequest)
		return
	}

	entry := &ObjectEntry{
		ID:        req.ObjectID,
		Subject:   req.Subject,
		ClassName: req.ClassName,
		LastSeen:  time.Now(),
	}

	c.mu.Lock()
	c.registry[req.ObjectID] = entry
	c.mu.Unlock()
	c.markRegistryDirty()

	// Signal any goroutine waiting for this object to appear
	if c.traceEnabled.Load() { _, _ = c.callObjectID(req.ObjectID, "__trace:", []Value{StrVal("start")}, inspectionTimeout) } else {}
	if ch, ok := c.spawnWaiters.Load(req.ObjectID); ok {
		select {
		case ch.(chan error) <- nil:
		default:
		}
	}

	logInfo("[coordinator] registered %s (%s) at %s\n", req.ObjectID, req.ClassName, req.Subject)
	w.WriteHeader(http.StatusOK)
}

func (c *Coordinator) handleSpawnFailed(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req struct {
		ObjectID string `json:"objectId"`
		Error    string `json:"error"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	w.WriteHeader(http.StatusOK)
	if ch, ok := c.spawnWaiters.Load(req.ObjectID); ok {
		select {
		case ch.(chan error) <- fmt.Errorf("%s", req.Error):
		default:
		}
	}
}

func (c *Coordinator) handleDeregister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req DeregisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad deregister request: "+err.Error(), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	entry, ok := c.registry[req.ObjectID]
	if ok {
		delete(c.registry, req.ObjectID)
	}
	c.mu.Unlock()

	if ok {
		c.markRegistryDirty()
		logInfo("[coordinator] deregistered %s (%s)\n", req.ObjectID, entry.ClassName)
	}
	w.WriteHeader(http.StatusOK)
}

func (c *Coordinator) handleRegistry(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	entries := make([]ObjectEntry, 0, len(c.registry))
	for _, e := range c.registry {
		entries = append(entries, *e)
	}
	c.mu.RUnlock()
	json.NewEncoder(w).Encode(HeartbeatResponse{Registry: entries})
}

func (c *Coordinator) handleMetrics(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	entries := make([]ObjectEntry, 0, len(c.registry))
	for _, e := range c.registry {
		entries = append(entries, *e)
	}
	registrySize := len(c.registry)
	c.mu.RUnlock()

	c.rootsMu.Lock()
	rootCount := len(c.roots)
	c.rootsMu.Unlock()

	c.workerMu.RLock()
	workerCount := len(c.workers)
	c.workerMu.RUnlock()

	resp := MetricsResponse{
		RegistrySize: registrySize,
		RootCount:    rootCount,
		WorkerCount:  workerCount,
		Runtime:      runtimeMetricsSnapshot(),
		Coordinator:  c.coordinatorMetricsSnapshot(),
	}

	for _, entry := range entries {
		if entry.ID == coordinatorID {
			continue
		}
		reply, err := c.sendObjectID(entry.ID, "__metricsJson", nil, 500*time.Millisecond)
		if err != nil || reply.Signaled || reply.Result.Kind != "string" {
			resp.Objects = append(resp.Objects, ObjectMetrics{
				ObjectID:  entry.ID,
				ClassName: entry.ClassName,
				Subject:   entry.Subject,
				Error:     fmt.Sprintf("metrics unavailable: %v", err),
			})
			continue
		}
		var objMetrics ObjectMetrics
		if err := json.Unmarshal([]byte(reply.Result.Str), &objMetrics); err != nil {
			resp.Objects = append(resp.Objects, ObjectMetrics{
				ObjectID:  entry.ID,
				ClassName: entry.ClassName,
				Subject:   entry.Subject,
				Error:     "metrics decode: " + err.Error(),
			})
			continue
		}
		resp.Objects = append(resp.Objects, objMetrics)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (c *Coordinator) handleSpawn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req SpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad spawn request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Use explicit pulse timeout from request, or fall back to a default.
	// Blocks no longer have a shorter timeout — reachability decides their
	// fate just like every other object.
	pulseTimeoutSecs := req.PulseTimeoutSecs
	if pulseTimeoutSecs == 0 {
		pulseTimeoutSecs = envInt("SMT_PULSE_TIMEOUT_SECS", 60)
	}

	resp, err := c.spawnObject(req.ClassName, req.BlockDef, req.AutoInit, pulseTimeoutSecs, req.Snapshot)
	if err != nil {
		json.NewEncoder(w).Encode(SpawnResponse{Error: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(resp)
}

func (c *Coordinator) handleTranscript(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req TranscriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad transcript request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Pretty-print with class name instead of raw ID
	label := req.ObjectID
	c.mu.RLock()
	if entry, ok := c.registry[req.ObjectID]; ok {
		label = entry.ClassName
	}
	c.mu.RUnlock()

	fmt.Printf("[%s] %s\n", label, req.Text)
	w.WriteHeader(http.StatusOK)
}

// spawnObject starts a new object process and waits for it to register.
// className: class to instantiate (use "Array" or "__block__" for built-ins)
// blockDef:  inline block definition (only for __block__); written to temp file locally
// autoInit:  pass --init flag so the process self-initializes on startup
// pulseTimeoutSecs:  idle timeout before the process exits (0 = never)
func (c *Coordinator) spawnObject(className string, blockDef *BlockDef, autoInit bool, pulseTimeoutSecs int, snapshots ...*NativeSnapshot) (SpawnResponse, error) {
	snapshot := nativeSnapshotArgument(snapshots)
	if snapshot != nil {
		if err := validateNativeSnapshot(snapshot); err != nil { return SpawnResponse{}, err } else if autoInit || blockDef != nil || userToBuiltinClass(className) != snapshot.Definition.Name { return SpawnResponse{}, fmt.Errorf("clone spawn requires matching class, no initializer and no separate block definition") } else {}
	} else {}
	start := time.Now()
	var resp SpawnResponse
	var err error
	defer func() {
		c.recordSpawnLatency(start, err)
	}()
	worker := c.reserveWorker()
	if worker != nil {
		defer c.releaseWorker(worker.ID)
		resp, err = c.spawnOnWorker(worker, className, blockDef, autoInit, pulseTimeoutSecs, snapshot)
		return resp, err
	}

	resp, err = c.spawnLocal(className, blockDef, autoInit, pulseTimeoutSecs, snapshot)
	return resp, err
}

// spawnLocal starts an object process on this machine via exec.Command.
func (c *Coordinator) spawnLocal(className string, blockDef *BlockDef, autoInit bool, pulseTimeoutSecs int, snapshots ...*NativeSnapshot) (SpawnResponse, error) {
	id := newUUID()

	// Register the waiter BEFORE spawning to avoid any race
	ch := make(chan error, 1)
	c.spawnWaiters.Store(id, ch)
	defer c.spawnWaiters.Delete(id)

	bin, err := os.Executable()
	if err != nil {
		return SpawnResponse{}, fmt.Errorf("cannot locate binary: %w", err)
	}

	// User-facing aliases for built-in classes — see builtins.go.
	spawnClass := userToBuiltinClass(className)

	args := []string{
		"object",
		"--class", spawnClass,
		"--id", id,
		"--coordinator", c.myURL,
		"--pulse-timeout", strconv.Itoa(pulseTimeoutSecs),
	}
	args = append(args, ClassPathArgs()...)
	if autoInit {
		args = append(args, "--init")
	}

	// Write inline BlockDef to a local temp file for the child process
	if blockDef != nil {
		data, err := json.Marshal(blockDef)
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("marshal block def: %w", err)
		}
		tmp, err := os.CreateTemp("", "smt-block-"+id+"-*.json")
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("create block def: %w", err)
		}
		defFile := tmp.Name()
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return SpawnResponse{}, fmt.Errorf("write block def: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return SpawnResponse{}, fmt.Errorf("close block def: %w", err)
		}
		args = append(args, "--def", defFile)
	}

	cmd := exec.Command(bin, args...)
	if snapshot := nativeSnapshotArgument(snapshots); snapshot != nil {
		file, err := snapshotArgumentFile(snapshot)
		if err != nil { return SpawnResponse{}, err } else {}
		defer os.Remove(file)
		cmd.Args = append(cmd.Args, "--snapshot", file)
	} else {}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return SpawnResponse{}, fmt.Errorf("failed to start %s: %w", className, err)
	}
	child := &localChild{id: id, done: make(chan error, 1)}
	c.childrenMu.Lock()
	c.children[id] = child
	c.childrenMu.Unlock()
	go func() {
		err := cmd.Wait()
		c.childrenMu.Lock()
		if c.children[id] == child {
			delete(c.children, id)
		}
		c.childrenMu.Unlock()
		child.done <- err
	}()

	select {
	case err := <-ch:
		if err != nil {
			return SpawnResponse{}, err
		}
		return SpawnResponse{ObjectID: id, Subject: natsSubjectForObject(id)}, nil
	case <-time.After(spawnTimeout()):
		return SpawnResponse{}, fmt.Errorf("timeout waiting %s for %s object %s to register", spawnTimeout(), className, id)
	}
}

func (c *Coordinator) reserveWorker() *WorkerEntry {
	c.workerMu.Lock()
	defer c.workerMu.Unlock()
	var best *WorkerEntry
	for _, worker := range c.workers {
		if best == nil ||
			worker.InFlightSpawns < best.InFlightSpawns ||
			(worker.InFlightSpawns == best.InFlightSpawns && worker.AssignedSpawns < best.AssignedSpawns) ||
			(worker.InFlightSpawns == best.InFlightSpawns && worker.AssignedSpawns == best.AssignedSpawns && worker.ID < best.ID) {
			best = worker
		}
	}
	if best == nil {
		return nil
	}
	best.InFlightSpawns++
	best.AssignedSpawns++
	return &WorkerEntry{
		ID:             best.ID,
		Host:           best.Host,
		Port:           best.Port,
		InFlightSpawns: best.InFlightSpawns,
		AssignedSpawns: best.AssignedSpawns,
		LastSeen:       best.LastSeen,
	}
}

func (c *Coordinator) releaseWorker(workerID string) {
	c.workerMu.Lock()
	defer c.workerMu.Unlock()
	worker, ok := c.workers[workerID]
	if !ok || worker.InFlightSpawns == 0 {
		return
	}
	worker.InFlightSpawns--
}

// spawnOnWorker delegates object spawning to a reserved worker.
func (c *Coordinator) spawnOnWorker(worker *WorkerEntry, className string, blockDef *BlockDef, autoInit bool, pulseTimeoutSecs int, snapshots ...*NativeSnapshot) (SpawnResponse, error) {
	req := SpawnRequest{
		Snapshot:         nativeSnapshotArgument(snapshots),
		ClassName:        className,
		BlockDef:         blockDef,
		AutoInit:         autoInit,
		PulseTimeoutSecs: pulseTimeoutSecs,
	}
	bundle, err := classBundleFromClassPath()
	if err != nil {
		return SpawnResponse{}, fmt.Errorf("class source bundle: %w", err)
	}
	req.SourceBundle = bundle
	body, _ := json.Marshal(req)
	resp, err := rpcClient.Post(worker.URL()+"/spawn", "application/json", bytes.NewReader(body))
	if err != nil {
		return SpawnResponse{}, fmt.Errorf("worker %s unreachable: %w", worker.ID, err)
	}
	defer resp.Body.Close()

	var sr SpawnResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return SpawnResponse{}, fmt.Errorf("worker %s spawn response decode error: %w", worker.ID, err)
	}
	if sr.Error != "" {
		return SpawnResponse{}, fmt.Errorf("worker %s spawn error: %s", worker.ID, sr.Error)
	}
	return sr, nil
}

// ---- Pulse-GC roots ----

// addRoot marks an object as a pulse root.  Roots receive pulse: from
// the coordinator every pulseInterval and propagate from there.
func (c *Coordinator) addRoot(id string) {
	c.rootsMu.Lock()
	c.roots[id] = struct{}{}
	c.rootsMu.Unlock()
	c.markRegistryDirty()
}

// removeRoot un-marks an object — its subtree starts dying on the next
// pulseTimeout window.
func (c *Coordinator) removeRoot(id string) {
	c.rootsMu.Lock()
	delete(c.roots, id)
	c.rootsMu.Unlock()
	c.markRegistryDirty()
}

// ---- Registry persistence ----

// persistedRegistry is the on-disk shape written when registryFile is set:
// the live objects (minus the coordinator itself) plus the current pulse
// root set.  Roots are persisted because they can't be recomputed — a
// pinned service object isn't reachable from anything the reload sees.
type persistedRegistry struct {
	Registry []ObjectEntry `json:"registry"`
	Roots    []string      `json:"roots"`
}

// markRegistryDirty flags that the registry changed and should be
// re-snapshotted on the next persist tick.  No-op when persistence is off.
func (c *Coordinator) markRegistryDirty() {
	if c.registryFile != "" {
		c.registryDirty.Store(true)
	}
}

// persistLoop writes the registry to disk on a short interval whenever
// it's been marked dirty.  Coalescing through the dirty flag keeps a
// burst of registrations to a single write.
func (c *Coordinator) persistLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if c.registryDirty.Swap(false) {
			c.persistRegistryNow()
		}
	}
}

// persistRegistryNow snapshots the registry + roots and writes them to
// registryFile via a temp-file rename so a reader never sees a partial
// file.  Excludes the coordinator's own entry — it's recreated with the
// current port on every startup.
func (c *Coordinator) persistRegistryNow() {
	c.mu.RLock()
	entries := make([]ObjectEntry, 0, len(c.registry))
	for id, e := range c.registry {
		if id == coordinatorID {
			continue
		}
		entries = append(entries, *e)
	}
	c.mu.RUnlock()

	c.rootsMu.Lock()
	roots := make([]string, 0, len(c.roots))
	for id := range c.roots {
		roots = append(roots, id)
	}
	c.rootsMu.Unlock()

	data, err := json.MarshalIndent(persistedRegistry{Registry: entries, Roots: roots}, "", "  ")
	if err != nil {
		return
	}
	tmp := c.registryFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		logInfo("[coordinator] registry persist failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, c.registryFile); err != nil {
		logInfo("[coordinator] registry persist rename failed: %v\n", err)
	}
}

// loadRegistry reads a previously-persisted registry, re-validates each
// entry by pinging it (concurrently), and re-adopts the survivors —
// re-pinning any roots that are still alive.  Dead entries are dropped
// silently; this is best-effort adoption, not a guarantee.  Absent file
// (first run) is not an error.
func (c *Coordinator) loadRegistry() {
	data, err := os.ReadFile(c.registryFile)
	if err != nil {
		return // first run, or nothing persisted yet
	}
	var snap persistedRegistry
	if err := json.Unmarshal(data, &snap); err != nil {
		logInfo("[coordinator] registry file %s unreadable: %v\n", c.registryFile, err)
		return
	}

	live := make(chan ObjectEntry, len(snap.Registry))
	var wg sync.WaitGroup
	for _, e := range snap.Registry {
		if e.ID == coordinatorID {
			continue
		}
		wg.Add(1)
		go func(e ObjectEntry) {
			defer wg.Done()
			if _, err := sendNATSCall(e.Subject, "class", nil, 2*time.Second); err != nil {
				return
			}
			live <- e
		}(e)
	}
	wg.Wait()
	close(live)

	c.mu.Lock()
	for e := range live {
		e.LastSeen = time.Now()
		entry := e
		c.registry[e.ID] = &entry
	}
	n := len(c.registry) - 1 // minus coordinator
	c.mu.Unlock()

	rootCount := 0
	for _, id := range snap.Roots {
		c.mu.RLock()
		_, ok := c.registry[id]
		c.mu.RUnlock()
		if ok {
			c.addRoot(id)
			rootCount++
		}
	}
	if n > 0 || rootCount > 0 {
		logInfo("[coordinator] reloaded %d live object(s) and %d root(s) from %s\n", n, rootCount, c.registryFile)
	}
}

// pulseLoop is the heartbeat of pulse-based GC.  Every pulseInterval it
// mints a fresh sweep id (UnixMilli) and sends pulse: to every current
// root; recipients update their lastSweepID and propagate via pulse:.
func (c *Coordinator) pulseLoop() {
	ticker := time.NewTicker(c.pulseInterval)
	defer ticker.Stop()
	for range ticker.C {
		sweepID := time.Now().UnixMilli()
		c.rootsMu.Lock()
		ids := make([]string, 0, len(c.roots))
		for id := range c.roots {
			ids = append(ids, id)
		}
		c.rootsMu.Unlock()
		for _, id := range ids {
			go c.pulseRoot(id, sweepID)
		}
	}
}

// pulseRoot fire-and-forget sends pulse: to one root.  A failed call
// just means that root is gone (or temporarily unreachable) — it'll be
// retried next interval, and if it stays gone, the eviction loop drops
// it from the registry and removeRoot can clean up the root list.
func (c *Coordinator) pulseRoot(id string, sweepID int64) {
	start := time.Now()
	c.mu.RLock()
	entry, ok := c.registry[id]
	c.mu.RUnlock()
	if !ok {
		c.recordPulseLatency(start, fmt.Errorf("root %s missing", id))
		c.removeRoot(id) // root has died; stop trying to wake it
		return
	}
	_, err := sendNATSCall(entry.Subject, "pulse:", []Value{NumVal(float64(sweepID))}, 3*time.Second)
	c.recordPulseLatency(start, err)
}

// stalePruneLoop catches the failure mode pulse-GC can't see:
// objects that hard-crashed before they could `/deregister` and
// aren't reachable from any live pulse path.  We snapshot the
// registry at the start of each cycle, then ping the entries one
// at a time at stalePruneInterval, evicting any that don't answer.
//
// One ping per interval — by design — so the coordinator's
// outbound traffic stays flat regardless of how many objects are
// alive.  At the default 1s interval, 20K objects take ~5.5h for
// a full pass; small clusters get cycled in seconds.
func (c *Coordinator) stalePruneLoop() {
	ticker := time.NewTicker(c.stalePruneInterval)
	defer ticker.Stop()
	var queue []string
	for range ticker.C {
		if len(queue) == 0 {
			queue = c.snapshotRegistryIDs()
		}
		if len(queue) == 0 {
			continue
		}
		id := queue[0]
		queue = queue[1:]
		c.stalePruneOne(id)
	}
}

// snapshotRegistryIDs returns every object ID currently in the
// registry except the coordinator itself.  Used as the work queue
// for one stale-prune pass.
func (c *Coordinator) snapshotRegistryIDs() []string {
	c.mu.RLock()
	ids := make([]string, 0, len(c.registry))
	for id := range c.registry {
		if id == coordinatorID {
			continue
		}
		ids = append(ids, id)
	}
	c.mu.RUnlock()
	return ids
}

// stalePruneOne pings one object; on failure, evicts it from the
// registry and from the root set.  Treats any HTTP error or
// non-2xx as "dead" — there's no two-strikes guard, so a transient
// network blip will lose an object.  Acceptable trade-off given
// that the alternative (heartbeats) was rejected at scale.
// A failure now starts a five-second suspicion window; a successful probe
// clears it. This lets live objects reconnect after a brief broker outage.
func (c *Coordinator) stalePruneOne(id string) {
	c.mu.RLock()
	entry, ok := c.registry[id]
	c.mu.RUnlock()
	if !ok {
		return // already gone (deregistered between snapshot and now)
	}

	if _, err := sendNATSCall(entry.Subject, "class", nil, 3*time.Second); err == nil {
		c.clearStaleFailure(id)
		return // alive
	}
	if !c.confirmStaleFailure(id, time.Now()) {
		return
	} else {
		// Repeated failure beyond the reconnect window permits eviction.
	}

	c.mu.Lock()
	if _, still := c.registry[id]; still {
		delete(c.registry, id)
		delete(c.staleFailures, id)
		logInfo("[coordinator] stale-prune evicted %s (%s) at %s — unreachable\n",
			id, entry.ClassName, entry.Subject)
	}
	c.mu.Unlock()
	c.removeRoot(id)
}

// ---- Workspace (persistent REPL object) ----

// ensureWorkspace returns the workspace object ID, spawning it if needed.
func (c *Coordinator) ensureWorkspace() (string, error) {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()

	if c.workspaceID != "" {
		c.mu.RLock()
		_, alive := c.registry[c.workspaceID]
		c.mu.RUnlock()
		if alive {
			return c.workspaceID, nil
		}
		c.workspaceID = ""
	}

	if id, err := c.recoveredWorkspace(); err != nil {
		return "", err
	} else if id != "" {
		c.workspaceID = id
		return id, nil
	} else {
		// No surviving workspace root was loaded from the registry.
	}

	// Workspace pulse-timeout = 0 (no self-shutdown).  It's a root: the
	// coordinator's pulseLoop is what keeps it alive.  Without that the
	// workspace would die at every interval, which is fine, but
	// suppressing the timeout makes the REPL robust against a slow root
	// pulse loop or a brief coordinator hiccup.
	resp, err := c.spawnObject(BuiltinClassWorkspace, nil, false, 0)
	if err != nil {
		return "", fmt.Errorf("spawn workspace: %w", err)
	}
	c.workspaceID = resp.ObjectID
	c.addRoot(resp.ObjectID)
	logInfo("[coordinator] workspace started as %s at %s\n", resp.ObjectID, resp.Subject)
	return c.workspaceID, nil
}

// handleEval accepts a code string, forwards it to the workspace, and returns output.
func (c *Coordinator) handleEval(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())

	var req EvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(EvalResponse{Error: "bad eval request: " + err.Error()})
		return
	}

	wsID, err := c.ensureWorkspace()
	if err != nil {
		json.NewEncoder(w).Encode(EvalResponse{Error: err.Error()})
		return
	}

	c.mu.RLock()
	entry, ok := c.registry[wsID]
	c.mu.RUnlock()
	if !ok {
		c.workspaceMu.Lock()
		c.workspaceID = ""
		c.workspaceMu.Unlock()
		json.NewEncoder(w).Encode(EvalResponse{Error: "workspace not found in registry"})
		return
	}

	callResp, err := c.callObject(entry, "eval:", []Value{StrVal(req.Code)}, evalTimeout())
	if err != nil {
		json.NewEncoder(w).Encode(EvalResponse{Error: fmt.Sprintf("workspace unreachable: %v", err)})
		return
	}
	json.NewEncoder(w).Encode(EvalResponse{Output: callResp.Str})
}

// handleSelfInspect serves GET /inspect on the coordinator's own HTTP server.
// A query id inspects another object through its NATS mailbox.
func (c *Coordinator) handleSelfInspect(w http.ResponseWriter, r *http.Request) {
	if id := r.URL.Query().Get("id"); id != "" && id != coordinatorID {
		c.writeObjectInspect(w, id)
		return
	}

	c.mu.RLock()
	n := len(c.registry) - 1
	if n < 0 {
		n = 0
	}
	c.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(InspectResponse{
		ObjectID:  coordinatorID,
		ClassName: BuiltinClassCoordinator,
		Methods: []string{
			"allObjects", "objectsOfClass:", "objectAt:", "classNameOf:",
			"subjectOf:", "size", "printRegistry", "spawn:", "deregister:",
			"includesObject:", "class", "pin:", "unpin:", "isPinned:",
		},
		InstanceVars: map[string]Value{
			"liveObjects": NumVal(float64(n)),
		},
	})
}

func (c *Coordinator) handleObjectDebug(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/object/")
	id, action, ok := strings.Cut(rest, "/")
	if !ok || id == "" || action == "" {
		http.Error(w, "expected /object/<id>/(inspect|call|message|pulse|ping)", http.StatusNotFound)
		return
	}

	switch action {
	case "inspect":
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		c.writeObjectInspect(w, id)
	case "call":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
		var req CallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad object call request: "+err.Error(), http.StatusBadRequest)
			return
		}
		result, err := c.callObjectID(id, req.Method, req.Args, 30*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallResponse{Result: result})
	case "message":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
		var req MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad object message request: "+err.Error(), http.StatusBadRequest)
			return
		}
		reply, err := c.sendObjectID(id, req.Method, req.Args, 30*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		reply.MsgID = req.MsgID
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reply)
	case "pulse":
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
		var req PulseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad object pulse request: "+err.Error(), http.StatusBadRequest)
			return
		}
		result, err := c.callObjectID(id, "pulse:", []Value{NumVal(float64(req.SweepID))}, 5*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CallResponse{Result: result})
	case "ping":
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		if _, err := c.callObjectID(id, "class", nil, 3*time.Second); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "unknown object debug action", http.StatusNotFound)
	}
}

func (c *Coordinator) writeObjectInspect(w http.ResponseWriter, id string) {
	inspection, err := c.inspectObject(id, true)
	if err != nil {
		http.Error(w, "inspect failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(inspection)
}

func (c *Coordinator) callObjectID(id string, method string, args []Value, timeout time.Duration) (Value, error) {
	reply, err := c.sendObjectID(id, method, args, timeout)
	if err != nil {
		return Nil, err
	}
	if reply.Signaled {
		return Nil, fmt.Errorf("remote signal: %s", reply.SignalMsg)
	}
	return reply.Result, nil
}

func (c *Coordinator) sendObjectID(id string, method string, args []Value, timeout time.Duration) (ReplyRequest, error) {
	c.mu.RLock()
	entry, ok := c.registry[id]
	c.mu.RUnlock()
	if !ok {
		return ReplyRequest{}, fmt.Errorf("object %s not found", id)
	}
	return sendNATSCall(entry.Subject, method, args, timeout)
}

// handleUI serves the web REPL interface.
func (c *Coordinator) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, uiHTML)
}

// ---- Coordinator as a Smalltalk object ----

// handleMessage makes the coordinator respond to Smalltalk messages.
// Objects reach it via (send coordinator <method> ...) or any explicit send
// to ObjVal(coordinatorID).  The reply is posted back to the caller's /reply.
func (c *Coordinator) handleMessage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad coordinator message request: "+err.Error(), http.StatusBadRequest)
		return
	}
	reply := dispatchToReply(req.MsgID, func() Value {
		return c.dispatchCoordinatorMethod(req.Method, req.Args)
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

// dispatchCoordinatorMethod handles the coordinator's Smalltalk API.
func (c *Coordinator) dispatchCoordinatorMethod(method string, args []Value) Value {
	switch method {

	// allObjects → Array of objids for every live non-coordinator object
	case "allObjects":
		return c.coordBuildArray(func(e *ObjectEntry) bool {
			return e.ID != coordinatorID
		})

	// objectsOfClass: "ClassName" → Array of matching objids.  Accepts
	// either the user-facing name ("Process") or the internal one
	// ("__process__") — coordinator registers built-ins under the
	// internal name, so user code passing "Process" would otherwise
	// miss its own jobs.
	case "objectsOfClass:":
		if len(args) < 1 {
			return Nil
		}
		name := userToBuiltinClass(args[0].String())
		return c.coordBuildArray(func(e *ObjectEntry) bool {
			return e.ClassName == name && e.ID != coordinatorID
		})

	// objectAt: "ClassName" → first matching objid, or nil.  Same
	// user/internal-name acceptance as objectsOfClass:.
	case "objectAt:":
		if len(args) < 1 {
			return Nil
		}
		name := userToBuiltinClass(args[0].String())
		c.mu.RLock()
		defer c.mu.RUnlock()
		for id, e := range c.registry {
			if e.ClassName == name && id != coordinatorID {
				return ObjVal(id)
			}
		}
		return Nil

	// classNameOf: anObjId → String
	case "classNameOf:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return Nil
		}
		c.mu.RLock()
		e, ok := c.registry[args[0].ObjID]
		c.mu.RUnlock()
		if !ok {
			return Nil
		}
		return StrVal(e.ClassName)

	// subjectOf: anObjId → String
	case "subjectOf:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return Nil
		}
		c.mu.RLock()
		e, ok := c.registry[args[0].ObjID]
		c.mu.RUnlock()
		if !ok {
			return Nil
		}
		return StrVal(e.Subject)

	// size → Number of live objects (excluding coordinator)
	case "size":
		c.mu.RLock()
		n := len(c.registry) - 1
		c.mu.RUnlock()
		if n < 0 {
			n = 0
		}
		return NumVal(float64(n))

	// printRegistry — prints all objects to coordinator stdout
	case "printRegistry":
		c.mu.RLock()
		entries := make([]*ObjectEntry, 0, len(c.registry))
		for _, e := range c.registry {
			if e.ID != coordinatorID {
				entries = append(entries, e)
			}
		}
		c.mu.RUnlock()
		fmt.Printf("[coordinator] --- registry: %d object(s) ---\n", len(entries))
		for _, e := range entries {
			fmt.Printf("[coordinator]   %-20s  %-18s  %s\n", e.ClassName, e.ID, e.Subject)
		}
		return Nil

	// spawn: "ClassName" → objid  (like new but initiated from coordinator)
	case "spawn:":
		if len(args) < 1 || args[0].Kind != "string" {
			return Nil
		}
		resp, err := c.spawnObject(args[0].Str, nil, false, 60)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[coordinator] spawn: failed: %v\n", err)
			return Nil
		}
		return ObjVal(resp.ObjectID)

	// pin: anObjId — make the object a pulse-GC root, so it (and everything
	// it transitively references) stays alive even when nothing in the
	// workspace holds a reference.  The escape hatch for long-lived service
	// objects: an HttpServer, a connection pool, a cache.  Returns the id
	// so `srv := coordinator pin: HttpServer new` reads cleanly.
	case "pin:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return Nil
		}
		c.addRoot(args[0].ObjID)
		logInfo("[coordinator] pinned %s\n", args[0].ObjID)
		return args[0]

	// unpin: anObjId — drop it from the root set.  Its subtree starts
	// dying on the next pulseTimeout window unless another root reaches it.
	case "unpin:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return Nil
		}
		c.removeRoot(args[0].ObjID)
		logInfo("[coordinator] unpinned %s\n", args[0].ObjID)
		return args[0]

	// isPinned: anObjId → bool — whether the object is currently a root.
	case "isPinned:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return False
		}
		c.rootsMu.Lock()
		_, ok := c.roots[args[0].ObjID]
		c.rootsMu.Unlock()
		return BoolVal(ok)

	// deregister: anObjId — removes from registry (process cleans up on idle)
	case "deregister:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return Nil
		}
		id := args[0].ObjID
		c.mu.Lock()
		entry, ok := c.registry[id]
		if ok {
			delete(c.registry, id)
		}
		c.mu.Unlock()
		if ok {
			c.markRegistryDirty()
			logInfo("[coordinator] deregister: removed %s (%s)\n", id, entry.ClassName)
		}
		return Nil

	// includesObject: anObjId → bool
	case "includesObject:":
		if len(args) < 1 || args[0].Kind != "objid" {
			return False
		}
		c.mu.RLock()
		_, ok := c.registry[args[0].ObjID]
		c.mu.RUnlock()
		return BoolVal(ok)

	// class → "coordinator" for uniformity
	case "class":
		return StrVal(BuiltinClassCoordinator)

	// help → starter-card string; printHelp → also dumps to transcript.
	// Same text the terminal REPL shows for `help`.
	case "help":
		return StrVal(helpText())
	case "printHelp":
		for _, line := range strings.Split(helpText(), "\n") {
			fmt.Println(line)
		}
		return Nil

	// shutdown — graceful coordinator exit.  Asks every locally
	// spawned object subprocess to shut down (the same path --eval
	// uses on completion), then os.Exit(0) the coordinator process
	// itself after a brief flush window so the HTTP response carrying
	// the OK can drain.  Application's own exit endpoint — not a shell
	// kill from outside.
	case "shutdown":
		go func() {
			c.shutdownLocalChildren(5 * time.Second)
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
		return StrVal("coordinator: shutting down")

	default:
		return unsupportedMessage(ObjVal(coordinatorID), "Coordinator", method, args)
	}
}

// coordBuildArray spawns an Array and populates it with objids matching pred.
// Returns the Array's objid, or Nil on error.
func (c *Coordinator) coordBuildArray(pred func(*ObjectEntry) bool) Value {
	// Snapshot matching IDs while holding the lock
	c.mu.RLock()
	var ids []string
	for id, e := range c.registry {
		if pred(e) {
			ids = append(ids, id)
		}
	}
	c.mu.RUnlock()

	arrResp, err := c.spawnObject(BuiltinClassArray, nil, false, 60)
	if err != nil {
		return Nil
	}
	for _, id := range ids {
		_, _ = sendNATSCall(arrResp.Subject, "add:", []Value{ObjVal(id)}, 30*time.Second)
	}
	return ObjVal(arrResp.ObjectID)
}

// ---- Worker cluster management ----

// handleWorkerHeartbeat upserts the worker from a self-describing beat: the
// request carries host/port, so an unknown worker (a first beat, or any beat
// after a coordinator restart) is simply created.  No separate registration
// step or re-register handshake.
func (c *Coordinator) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, internalHTTPMaxRequestBytes())
	var req WorkerHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad worker heartbeat request: "+err.Error(), http.StatusBadRequest)
		return
	}

	c.workerMu.Lock()
	if entry, ok := c.workers[req.WorkerID]; ok {
		entry.Host = req.Host
		entry.Port = req.Port
		entry.LastSeen = time.Now()
	} else {
		c.workers[req.WorkerID] = &WorkerEntry{
			ID: req.WorkerID, Host: req.Host, Port: req.Port, InFlightSpawns: 0, AssignedSpawns: 0, LastSeen: time.Now(),
		}
		logInfo("[coordinator] worker registered: %s at %s:%d\n", req.WorkerID, req.Host, req.Port)
	}
	c.workerMu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// workerEvictionLoop removes workers that have not sent a heartbeat recently.
func (c *Coordinator) workerEvictionLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-30 * time.Second)
		c.workerMu.Lock()
		for id, entry := range c.workers {
			if entry.LastSeen.Before(cutoff) {
				logInfo("[coordinator] evicting dead worker %s (%s:%d)\n", id, entry.Host, entry.Port)
				delete(c.workers, id)
			}
		}
		c.workerMu.Unlock()
	}
}
