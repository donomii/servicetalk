package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Usage:
//
//   smt                          — start coordinator + interactive REPL
//   smt --eval EXPR              — one-shot eval, print result, exit
//   smt [--port N] [--main C]    — default action with overrides
//   smt coordinator [flags]      — coordinator in daemon / web-REPL mode
//   smt worker      [flags]      — cluster worker
//   smt object      [flags]      — internal: spawned object process
//
// The bare `smt` invocation is the one you want 99% of the time.  The
// subcommands are for cluster deployments and the object-spawn hook.

func main() {
	stopEmbeddedNATS, err := startEmbeddedNATS()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer stopEmbeddedNATS()
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "coordinator":
			runCoordinatorCmd(os.Args[2:])
			return
		case "worker":
			runWorkerCmd(os.Args[2:])
			return
		case "object":
			runObjectCmd(os.Args[2:])
			return
		case "prebuild":
			runPrebuildCmd(os.Args[2:])
			return
		case "shutdown":
			runShutdownCmd(os.Args[2:])
			return
		case "shell":
			runShellCmd(os.Args[2:])
			return
		case "repl":
			runReplCmd(os.Args[2:])
			return
		case "run":
			runRunCmd(os.Args[2:])
			return
		case "script":
			if len(os.Args) != 3 {
				fatalf("usage: smt script FILE")
			} else {
				os.Exit(runScript(os.Args[2], 0))
			}
		case "test":
			os.Exit(runTests(os.Args[2:]))
		case "build":
			runBuildCmd(os.Args[2:])
			return
		case "-h", "--help", "help":
			printUsage(os.Stdout)
			return
		}
	}
	runDefault(os.Args[1:])
}

// builtMain is injected by `smt build` via -ldflags "-X main.builtMain=Class".
// When set, a no-subcommand invocation of the built binary auto-spawns that
// class and serves (instead of dropping into the REPL), so a single-binary
// app is turn-key: copy the file, run it, the app starts.
var builtMain string

// pathFlag implements flag.Value for the repeatable --path DIR option.
// Each occurrence appends a directory to the classpath (after CWD and
// SMT_PATH).
type pathFlag []string

func (p *pathFlag) String() string     { return strings.Join(*p, string(filepath.ListSeparator)) }
func (p *pathFlag) Set(v string) error { *p = append(*p, v); return nil }

// runDefault is the entry point for `smt` with no subcommand.  It brings up
// a coordinator on localhost and either runs a one-shot eval (--eval) or
// drops into an interactive terminal REPL.
func runDefault(args []string) {
	fs := flag.NewFlagSet("smt", flag.ExitOnError)
	fs.Usage = func() { printUsage(fs.Output()) }
	port := fs.Int("port", 8000, "coordinator listen port")
	mainClass := fs.String("main", "", "class to auto-spawn on startup (empty = none)")
	eval := fs.String("eval", "", "evaluate this expression, print the result, and exit")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", false, "print coordinator/worker lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)
	SetClassPath(paths)

	mode := ModeRepl
	if *eval != "" {
		mode = ModeEval
	} else if *mainClass == "" && builtMain != "" {
		// A `smt build` single-binary with no subcommand: run the app
		// (spawn its main class and serve) rather than open a REPL.
		*mainClass = builtMain
		mode = ModeServe
	}
	runCoordinator(*port, *mainClass, mode, *eval)
}

func runCoordinatorCmd(args []string) {
	fs := flag.NewFlagSet("coordinator", flag.ExitOnError)
	port := fs.Int("port", 8000, "coordinator listen port")
	mainClass := fs.String("main", "Main", "class to spawn on startup (empty to skip)")
	eval := fs.String("eval", "", "evaluate this expression, print the result, and exit")
	repl := fs.Bool("repl", false, "drop into terminal REPL after startup instead of blocking")
	script := fs.String("script", "", "evaluate a script file, report failures, and exit")
	regFile := fs.String("registry-file", "", "persist the registry to this file and reload it on restart (also via SMT_REGISTRY_FILE)")
	pulseIntervalMs := fs.Int("pulse-interval", 0, "pulse GC interval in ms (0 = use SMT_PULSE_INTERVAL_MS or default 10000)")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", true, "print coordinator/worker lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)
	SetClassPath(paths)
	if *regFile != "" {
		os.Setenv("SMT_REGISTRY_FILE", *regFile)
	}
	if *pulseIntervalMs > 0 {
		os.Setenv("SMT_PULSE_INTERVAL_MS", strconv.Itoa(*pulseIntervalMs))
	}

	mode := ModeServe
	if *eval != "" {
		*mainClass = ""
		mode = ModeEval
	} else if *repl {
		mode = ModeRepl
	}
	if *script != "" {
		if *eval != "" || *repl {
			fatalf("coordinator: --script cannot be combined with --eval or --repl")
		} else if src, err := readScript(*script); err != nil {
			fatalf("%v", err)
		} else {
			SetClassPath(append([]string{filepath.Dir(*script)}, paths...))
			*mainClass, mode, *eval = "", ModeScript, src
		}
	}
	runCoordinator(*port, *mainClass, mode, *eval)
}

func runWorkerCmd(args []string) {
	fs := flag.NewFlagSet("worker", flag.ExitOnError)
	coord := fs.String("coordinator", "http://localhost:8000", "coordinator URL")
	host := fs.String("host", "", "advertised hostname (auto-detected if empty)")
	port := fs.Int("port", 8001, "worker listen port")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", true, "print worker lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)
	SetClassPath(paths)
	runWorker(*coord, *host, *port)
}

func runObjectCmd(args []string) {
	fs := flag.NewFlagSet("object", flag.ExitOnError)
	class := fs.String("class", "", "class name")
	id := fs.String("id", "", "object ID assigned by coordinator")
	coord := fs.String("coordinator", "http://localhost:8000", "coordinator URL")
	init := fs.Bool("init", false, "call initialize on startup (entry-point objects only)")
	def := fs.String("def", "", "block definition JSON file (internal use)")
	snapshot := fs.String("snapshot", "", "native clone snapshot JSON file (internal use)")
	pulseTimeout := fs.Int("pulse-timeout", 60, "shut down if no pulse received within this many seconds (0 disables)")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", false, "print lifecycle messages to stderr (child processes inherit via SMT_VERBOSE)")
	fs.Parse(args)
	setVerbose(verbose)
	SetClassPath(paths)

	if *class == "" || *id == "" {
		fmt.Fprintln(os.Stderr, "object: --class and --id are required")
		os.Exit(1)
	}
	runObject(*class, *id, *coord, *init, *def, *pulseTimeout, *snapshot)
}

// runReplCmd is the explicit `smt repl` form — same Smalltalk REPL
// the bare `smt` invocation gives you, but symmetric with `smt shell`
// so users don't have to remember which mode is the default.
func runReplCmd(args []string) {
	fs := flag.NewFlagSet("repl", flag.ExitOnError)
	port := fs.Int("port", 8000, "coordinator listen port")
	mainClass := fs.String("main", "", "class to auto-spawn on startup (empty = none)")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", false, "print coordinator/worker lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)
	SetClassPath(paths)
	runCoordinator(*port, *mainClass, ModeRepl, "")
}

// runShellCmd boots a coordinator + Shell instance and drops into a
// stdin-driven shell prompt.  Each line goes through `Shell run:`,
// captured output prints to stdout.  Ctrl-D / `exit` / `:quit` quits
// cleanly.
func runShellCmd(args []string) {
	fs := flag.NewFlagSet("shell", flag.ExitOnError)
	port := fs.Int("port", 8000, "coordinator listen port")
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", false, "print coordinator/worker lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)
	// Auto-add binary-relative examples/shell so the user doesn't have
	// to remember --path when running from a built tree or a bundle.
	setShellClassPath(paths)
	runCoordinator(*port, "", ModeShell, "")
}

func setShellClassPath(paths []string) {
	SetClassPath(paths)
	classPath = append(classPath, shellExamplePaths()...)
	userClassPath = append([]string{}, classPath...)
}

// shellExamplePaths returns binary-relative candidates for the
// examples/shell directory so `smt shell` Just Works without --path.
// Mirrors binaryRelativeFallbacks: exe-dir/examples/shell,
// exe-dir/../examples/shell, exe-dir/../Resources/examples/shell.
func shellExamplePaths() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	d := filepath.Dir(exe)
	return []string{
		filepath.Join(d, "examples", "shell"),
		filepath.Join(filepath.Dir(d), "examples", "shell"),
		filepath.Join(filepath.Dir(d), "Resources", "examples", "shell"),
	}
}

// runShutdownCmd POSTs to a coordinator's /shutdown endpoint, asking
// it to gracefully tear down its spawned object subprocesses and
// exit.  Application-level exit, not a shell kill.
func runShutdownCmd(args []string) {
	fs := flag.NewFlagSet("shutdown", flag.ExitOnError)
	port := fs.Int("port", 8000, "coordinator port to shut down")
	host := fs.String("host", "localhost", "coordinator host")
	fs.Parse(args)
	url := fmt.Sprintf("http://%s:%d/shutdown", *host, *port)
	resp, err := rpcClient.Post(url, "text/plain", bytes.NewReader(nil))
	if err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Print(string(body))
}

func runPrebuildCmd(args []string) {
	fs := flag.NewFlagSet("prebuild", flag.ExitOnError)
	var paths pathFlag
	fs.Var(&paths, "path", "directory to search for class files (repeatable; also via SMT_PATH)")
	fs.Parse(args)
	SetClassPath(paths)
	if err := PrebuildAllForeign(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  smt repl                     Smalltalk REPL (also the default with no args)")
	fmt.Fprintln(w, "  smt shell                    command shell (ls, cd, run, jobs, …) on stdin")
	fmt.Fprintln(w, "  smt --eval EXPR              one-shot eval, print, exit")
	fmt.Fprintln(w, "  smt shutdown [--port N]      gracefully stop the coordinator at that port")
	fmt.Fprintln(w, "  smt coordinator [flags]      coordinator only (daemon / web REPL)")
	fmt.Fprintln(w, "  smt worker --coordinator URL [flags]")
	fmt.Fprintln(w, "  smt object  --class C --id ID [flags]   (internal)")
	fmt.Fprintln(w, "  smt prebuild [--path DIR]    build every foreign class in classpath for current arch")
	fmt.Fprintln(w, "  smt run DIR [flags]          run the app in DIR (classpath + --main Main, then serve)")
	fmt.Fprintln(w, "  smt script FILE             evaluate a file in an isolated one-shot runtime")
	fmt.Fprintln(w, "  smt test [FILES...]         run scripts; discover *_test.smt / *Test.smt if omitted")
	fmt.Fprintln(w, "  smt build DIR -o BIN         compile a self-contained single binary (classes baked in)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  Common flags: --port N, --main CLASS, --path DIR")
}
