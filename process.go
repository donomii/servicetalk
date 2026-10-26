package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// Process is a built-in class.  Each instance owns one OS subprocess
// (started via `sh -c CMD` or via an explicit argv) and carries its
// state — captured stdout+stderr, exit code, status — alongside the
// rest of the cluster's live objects.  Because a Process IS a
// servicetalk object:
//
//   - `coordinator objectsOfClass: 'Process'` lists every running and
//     finished job in the cluster.  That's the equivalent of bash
//     `jobs` — but cluster-wide, surviving the shell that started it.
//   - The web REPL's Live Objects panel shows every Process by id /
//     host / class — point-and-click introspection of a fleet of jobs.
//   - A Process spawned on one worker is reachable from the
//     coordinator and from any other object on any worker.  No
//     bookkeeping required.
//
// Surface:
//
//   p := Process new.
//   p start: 'sleep 5'.                         "sh -c form (the usual case)"
//   p start: 'echo' args: (Array new add: 'hi'; yourself). "argv form, no shell"
//   p output.                "→ stdout+stderr captured so far (string)"
//   p outputLines.           "→ Array of lines"
//   p status.                "→ 'starting' | 'running' | 'exited' | 'error'"
//   p exitCode.              "→ number, or nil if not yet exited"
//   p pid.                   "→ OS pid"
//   p command.               "→ the command string passed to start:"
//   p signal: 'TERM'.        "→ kill(pid, SIGTERM); 'KILL', 'INT', 'HUP' too"
//   p wait.                  "→ blocks until exit, returns exit code"
//   p isRunning.             "→ bool"
//
// Output buffering: stdout and stderr are merged into one byte buffer
// up to processOutputCap (1 MiB).  Long-running jobs that spew more
// than that get truncated; the truncation is reflected in `output` as
// a final "...<truncated>" line so the caller can spot it.

const processOutputCap = 1 << 20

type processState struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	command  string
	stdout   *bytes.Buffer
	started  bool
	finished int32 // atomic
	exitCode int   // valid only when finished == 1
	exitErr  string
	done     chan struct{}
}

// callProcessMethod handles every send to a Process instance.
func (obj *ObjectServer) callProcessMethod(name string, args []Value) Value {
	switch name {
	case "initialize":
		return Nil
	case "class":
		return StrVal(UserClassProcess)
	case "yourself":
		return ObjVal(obj.id)
	case "isNil":
		return False
	case "notNil":
		return True
	case "respondsTo:":
		if len(args) < 1 {
			return False
		}
		switch args[0].String() {
		case "start:", "start:cwd:", "start:args:", "start:args:cwd:",
			"output", "outputLines", "status", "exitCode", "pid",
			"command", "signal:", "wait", "isRunning",
			"printString", "displayString":
			return True
		}
		return False
	case "printString", "displayString":
		return StrVal(processPrintString(obj.processState()))
	case "inspect":
		st := obj.processState()
		st.mu.Lock()
		obj.transcriptf("Process %s — %s — %d bytes captured", st.command, processStatus(st), st.stdout.Len())
		st.mu.Unlock()
		return Nil
	case "free":
		obj.processSignal("KILL")
		obj.deregister()
		return Nil

	case "start:":
		if len(args) < 1 {
			obj.transcriptf("Process start: requires a command string")
			return ObjVal(obj.id)
		}
		obj.processStartShell(args[0].String(), "")
		return ObjVal(obj.id)

	case "start:cwd:":
		if len(args) < 2 {
			obj.transcriptf("Process start:cwd: requires (command, dir)")
			return ObjVal(obj.id)
		}
		obj.processStartShell(args[0].String(), args[1].String())
		return ObjVal(obj.id)

	case "start:args:":
		if len(args) < 2 {
			obj.transcriptf("Process start:args: requires (command, Array)")
			return ObjVal(obj.id)
		}
		argv := obj.arrayToStrings(args[1])
		obj.processStartArgv(args[0].String(), argv, "")
		return ObjVal(obj.id)

	case "start:args:cwd:":
		if len(args) < 3 {
			obj.transcriptf("Process start:args:cwd: requires (command, Array, dir)")
			return ObjVal(obj.id)
		}
		argv := obj.arrayToStrings(args[1])
		obj.processStartArgv(args[0].String(), argv, args[2].String())
		return ObjVal(obj.id)

	case "output":
		st := obj.processState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return StrVal(st.stdout.String())

	case "outputLines":
		st := obj.processState()
		st.mu.Lock()
		raw := st.stdout.String()
		st.mu.Unlock()
		return obj.linesArray(raw)

	case "status":
		return StrVal(processStatus(obj.processState()))

	case "exitCode":
		st := obj.processState()
		if atomic.LoadInt32(&st.finished) == 0 {
			return Nil
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		return NumVal(float64(st.exitCode))

	case "pid":
		st := obj.processState()
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.cmd == nil || st.cmd.Process == nil {
			return NumVal(0)
		}
		return NumVal(float64(st.cmd.Process.Pid))

	case "command":
		st := obj.processState()
		st.mu.Lock()
		defer st.mu.Unlock()
		return StrVal(st.command)

	case "signal:":
		if len(args) < 1 {
			return False
		}
		return BoolVal(obj.processSignal(args[0].String()))

	case "wait":
		st := obj.processState()
		if !st.started {
			return Nil
		}
		<-st.done
		st.mu.Lock()
		defer st.mu.Unlock()
		return NumVal(float64(st.exitCode))

	case "isRunning":
		st := obj.processState()
		return BoolVal(st.started && atomic.LoadInt32(&st.finished) == 0)
	}

	obj.transcriptf("Process doesNotUnderstand: %s", name)
	return Nil
}

func (obj *ObjectServer) processState() *processState {
	if obj.procState == nil {
		obj.procState = &processState{
			stdout: &bytes.Buffer{},
			done:   make(chan struct{}),
		}
	}
	return obj.procState
}

func processStatus(st *processState) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	switch {
	case !st.started:
		return "starting"
	case atomic.LoadInt32(&st.finished) == 1 && st.exitErr != "":
		return "error"
	case atomic.LoadInt32(&st.finished) == 1:
		return "exited"
	default:
		return "running"
	}
}

func processPrintString(st *processState) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	status := "running"
	switch {
	case !st.started:
		status = "starting"
	case atomic.LoadInt32(&st.finished) == 1 && st.exitErr != "":
		status = "error"
	case atomic.LoadInt32(&st.finished) == 1:
		status = "exited"
	default:
		status = "running"
	}
	pid := 0
	if st.cmd != nil && st.cmd.Process != nil {
		pid = st.cmd.Process.Pid
	}
	return fmt.Sprintf("%-9s pid %-6d %s", status, pid, st.command)
}

// processStartShell wraps the command in `sh -c …` so the caller gets
// pipes / globs / quoting.  The argv form (start:args:) bypasses sh
// and runs the program directly, which is safer for caller-controlled
// arguments that might contain shell metacharacters.
//
// `cwd` is set on the spawned process when non-empty, so callers don't
// have to prepend `cd '…' &&` to their command line just to pin the
// working directory.
func (obj *ObjectServer) processStartShell(cmdLine, cwd string) {
	prog, argv := shellCommand(cmdLine)
	obj.processStartArgv(prog, argv, cwd)
	st := obj.processState()
	st.mu.Lock()
	st.command = cmdLine
	st.mu.Unlock()
}

func (obj *ObjectServer) processStartArgv(prog string, argv []string, cwd string) {
	st := obj.processState()
	st.mu.Lock()
	if st.started {
		st.mu.Unlock()
		obj.transcriptf("Process already started — spawn a new Process for a new command")
		return
	}
	st.command = prog
	if len(argv) > 0 {
		st.command = prog + " " + strings.Join(argv, " ")
	}
	cmd := exec.Command(prog, argv...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdout = newProcessWriter(st)
	cmd.Stderr = newProcessWriter(st)
	if err := cmd.Start(); err != nil {
		st.exitErr = err.Error()
		atomic.StoreInt32(&st.finished, 1)
		st.exitCode = -1
		close(st.done)
		st.mu.Unlock()
		obj.transcriptf("Process start failed: %v", err)
		return
	}
	st.cmd = cmd
	st.started = true
	st.mu.Unlock()

	go func() {
		err := cmd.Wait()
		st.mu.Lock()
		if err == nil {
			st.exitCode = 0
		} else if ee, ok := err.(*exec.ExitError); ok {
			st.exitCode = ee.ExitCode()
		} else {
			st.exitCode = -1
			st.exitErr = err.Error()
		}
		atomic.StoreInt32(&st.finished, 1)
		close(st.done)
		st.mu.Unlock()
	}()
}

// processWriter is the io.Writer plumbed into cmd.Stdout / cmd.Stderr.
// It appends to the captured buffer, capping at processOutputCap to
// keep a chatty job from blowing the heap.
type processWriter struct {
	st *processState
}

func newProcessWriter(st *processState) *processWriter {
	return &processWriter{st: st}
}

func (w *processWriter) Write(p []byte) (int, error) {
	w.st.mu.Lock()
	defer w.st.mu.Unlock()
	remaining := processOutputCap - w.st.stdout.Len()
	if remaining <= 0 {
		return len(p), nil // silently drop after cap
	}
	if len(p) > remaining {
		w.st.stdout.Write(p[:remaining])
		w.st.stdout.WriteString("\n…<truncated>\n")
		return len(p), nil
	}
	w.st.stdout.Write(p)
	return len(p), nil
}

func (obj *ObjectServer) processSignal(name string) bool {
	st := obj.processState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cmd == nil || st.cmd.Process == nil {
		return false
	}
	if atomic.LoadInt32(&st.finished) == 1 {
		return false
	}
	sig := signalFromName(name)
	if sig == nil {
		return false
	}
	return st.cmd.Process.Signal(sig) == nil
}

// arrayToStrings reads a servicetalk Array's elements one at a time
// via sendMessage and renders each via Value.String().  Used by
// start:args: to materialise an argv from a user-supplied Array.
func (obj *ObjectServer) arrayToStrings(v Value) []string {
	if v.Kind != "objid" {
		return nil
	}
	sz := obj.sendMessage(v, "size", nil)
	n := 0
	if sz.Kind == "number" {
		n = int(sz.Num)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		el := obj.sendMessage(v, "at:", []Value{NumVal(float64(i))})
		out = append(out, el.String())
	}
	return out
}

// linesArray converts captured output to an Array of lines.  Used by
// outputLines so callers can do `p outputLines do: [line | …]`.
func (obj *ObjectServer) linesArray(s string) Value {
	arrID := obj.spawnBuiltin(BuiltinClassArray)
	if arrID == "" {
		return Nil
	}
	ref := ObjVal(arrID)
	if s == "" {
		return ref
	}
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		obj.sendMessage(ref, "add:", []Value{StrVal(line)})
	}
	return ref
}

// avoid unused-import warning when this file is the only consumer of
// fmt — we keep fmt in case future helpers need formatting.
var _ = fmt.Sprintf
