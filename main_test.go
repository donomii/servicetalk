package main

import (
	"os"
	"testing"
)

// TestMain lets the compiled test binary double as the real `smt` binary
// when invoked with a subcommand.  The runtime spawns objects (and the
// integration test spawns a coordinator) by exec'ing os.Executable() —
// which, under `go test`, is this test binary.  Without this hook those
// re-invocations would just re-run the test suite.  A normal `go test`
// run has os.Args[1] like "-test.timeout=…", so the switch never matches.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "object":
			runObjectCmd(os.Args[2:])
			return
		case "coordinator", "script", "test":
			main()
			return
		case "worker":
			runWorkerCmd(os.Args[2:])
			return
		}
	}
	os.Exit(m.Run())
}
