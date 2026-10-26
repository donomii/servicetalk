//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScriptCompletionReapsBackgroundDescendants(t *testing.T) {
	for _, failure := range []bool{false, true} {
		dir := t.TempDir()
		pidfile := filepath.Join(dir, "child.pid")
		file := filepath.Join(dir, "background.smt")
		command := "echo $$ > " + pidfile + "; exec sleep 30"
		ending := "42"
		if failure {
			ending = "'failed after spawn' signal"
		}
		source := "p := Process new. p start: '" + strings.ReplaceAll(command, "'", "''") + "'. " + ending
		if err := os.WriteFile(file, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		out, err := scriptTestCommand(t, dir, "script", file)
		if (err != nil) != failure {
			t.Fatalf("background script returned unexpected status: %v: %s", err, out)
		}
		assertScriptChildStopped(t, pidfile, out)
	}
}

func TestScriptDeadlineCleansUpBlockedObjectAndChild(t *testing.T) {
	t.Setenv(natsStartupTimeoutVar, "1000")
	t.Setenv(spawnTimeoutVar, "1000")
	t.Setenv(evalTimeoutVar, "200")
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "child.pid")
	file := filepath.Join(dir, "blocked.smt")
	command := "echo $$ > " + pidfile + "; exec sleep 30"
	source := "p := Process new. p start: '" + strings.ReplaceAll(command, "'", "''") + "'. p wait"
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	out, err := scriptTestCommand(t, dir, "script", file)
	if err == nil || time.Since(started) > 16*time.Second {
		t.Fatalf("blocked script did not fail within its aggregate deadline: %v: %s", err, out)
	}
	assertScriptChildStopped(t, pidfile, out)
}

func assertScriptChildStopped(t *testing.T, pidfile, output string) {
	t.Helper()
	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("script did not start the child fixture: %v: %s", err, output)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		} else if stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil && strings.Contains(string(stat), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("script timeout left child %d alive", pid)
}
