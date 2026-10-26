package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// envInt returns the integer value of an env var, or fallback if unset
// or unparseable.  Used for tunables that are useful to override without
// adding a flag (e.g. pulse-GC timing during testing).
func envInt(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// envDuration reads an env var as an integer count of `unit` (e.g.
// "SMT_PULSE_INTERVAL_MS" with unit=time.Millisecond) and returns it
// as a duration, falling back to the supplied default.
func envDuration(name string, fallback time.Duration, unit time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * unit
		}
	}
	return fallback
}

// verbose is set by --verbose on any subcommand.  When false, lifecycle
// messages ("listening on port", "registered X", "workspace started", ...)
// are suppressed so command-line invocations like `smt --eval "1 + 2"`
// produce clean stdout.
//
// Real output — eval results, (print ...) transcripts, printRegistry,
// doesNotUnderstand — must NOT go through logInfo.  It's for diagnostic
// status only.
var verbose bool

// Child object/worker processes inherit verbosity via this env var, since
// they're launched via exec.Command and don't see the parent's flags.
const verboseEnv = "SMT_VERBOSE"

func init() {
	if os.Getenv(verboseEnv) == "1" {
		verbose = true
	}
}

// setVerbose is called after flag parsing in the parent process so that
// spawned child processes (objects, blocks) inherit the setting.
func setVerbose(v bool) {
	verbose = v
	if v {
		os.Setenv(verboseEnv, "1")
	} else {
		os.Unsetenv(verboseEnv)
	}
}

func logInfo(format string, args ...interface{}) {
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

