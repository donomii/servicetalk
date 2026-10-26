package main

import (
	"os"
	"testing"
)

func TestStartEmbeddedNATSProvidesBrokerForChildProcesses(t *testing.T) {
	t.Setenv(embedNATSVar, "1")
	t.Setenv(natsURLVar, "")
	stop, err := startEmbeddedNATS()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if got := os.Getenv(natsURLVar); got == "" || got == "nats://127.0.0.1:4222" {
		t.Fatalf("%s = %q, want the embedded broker URL", natsURLVar, got)
	}
	if got := os.Getenv(embedNATSVar); got != "" {
		t.Fatalf("%s = %q, want empty so child processes reuse the parent broker", embedNATSVar, got)
	}
}
