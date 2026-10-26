package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scriptTestCommand(t *testing.T, directory string, args ...string) (string, error) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "SMT_EMBED_NATS=", "SMT_NATS_URL=nats://127.0.0.1:1")
	configureScriptProcess(cmd)
	// Let the script runner cancel and reap its own child group first.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	defer func() {
		if cmd.Process != nil {
			cleanupScriptProcess(cmd)
		}
	}()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestScriptCommandAndExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		fail               bool
	}{
		{"success", "6 * 7", "42", false},
		{"empty", "", "nil", false},
		{"syntax", "[", "parse", true},
		{"signal", "'script failed' signal", "script failed", true},
		{"nested-block-signal", "[ 'nested block failed' signal ] value", "nested block failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "sample.smt")
			if err := os.WriteFile(file, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := scriptTestCommand(t, dir, "script", file)
			if (err != nil) != tc.fail || !strings.Contains(out, tc.want) {
				t.Fatalf("script returned %v, output %q; want failure=%v containing %q", err, out, tc.fail, tc.want)
			}
		})
	}
}

func TestTestCommandRejectsNestedMethodSignal(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"Failure.st": "class Failure [ initialize [ nil ] fail [ 'method failed' signal ] ]",
		"a_test.smt": "Failure new fail. Transcript show: 'must not continue'",
		"b_test.smt": "[ Failure new fail ] onError: [e | 42]",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		} else {
			// Exercise the real CLI and child processes.
		}
	}
	out, err := scriptTestCommand(t, dir, "test")
	if err == nil || !strings.Contains(out, "1 passed, 1 failed") || strings.Contains(out, "must not continue") {
		t.Fatalf("nested failure was not reported correctly: %v: %s", err, out)
	} else {
		// Handled errors still pass, and subsequent files still execute.
	}
}

func TestScriptFindsSiblingClass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Answer.st"), []byte("class Answer [ value [ ^ 42 ] ]"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "sample.smt")
	if err := os.WriteFile(file, []byte("Answer new value"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := scriptTestCommand(t, t.TempDir(), "script", file)
	if err != nil || !strings.Contains(out, "42") {
		t.Fatalf("script could not use sibling class: %v: %s", err, out)
	}
}

func TestTestCommandContinuesAfterFailureAndDiscoversScripts(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"a_test.smt": "'bad test' signal", "bTest.smt": "7 * 6"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := scriptTestCommand(t, dir, "test")
	if err == nil || !strings.Contains(out, "1 passed, 1 failed") || !strings.Contains(out, "ok   bTest") {
		t.Fatalf("test runner did not continue after failure: %v: %s", err, out)
	}
}

func TestScriptRejectsMissingFileAndEmptyTestDiscovery(t *testing.T) {
	dir := t.TempDir()
	out, err := scriptTestCommand(t, dir, "script", filepath.Join(dir, "absent.smt"))
	if err == nil || !strings.Contains(out, "read") {
		t.Fatalf("missing script: %v: %s", err, out)
	}
	out, err = scriptTestCommand(t, dir, "test")
	if err == nil || !strings.Contains(out, "no test files") {
		t.Fatalf("missing tests: %v: %s", err, out)
	}
}
