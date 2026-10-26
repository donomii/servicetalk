package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// runScript starts an ephemeral coordinator, evals file, prints output, returns exit code.
func runScript(file string, coordPort int) int {
	absolute, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "script: resolve %q: %v\n", file, err)
		return 1
	} else if _, err := readScript(absolute); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "script: locate runtime executable: %v\n", err)
		return 1
	} else {
		parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ctx, cancel := context.WithTimeout(parent, scriptTimeout())
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "coordinator", "--port", strconv.Itoa(coordPort), "--script", absolute, "--verbose=false")
		cmd.Env = scriptEnvironment()
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		configureScriptProcess(cmd)
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "script: start runtime for %q: %v\n", file, err)
			return 1
		} else {
			defer cleanupScriptProcess(cmd)
			if err := cmd.Wait(); err != nil {
				fmt.Fprintf(os.Stderr, "script: %q failed: %v (deadline: %v)\n", file, err, ctx.Err())
				return 1
			} else {
				return 0
			}
		}
	}
}

func readScript(file string) (string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("script: read %q: %w", file, err)
	} else if _, err := parseWorkspaceSource(string(src)); err != nil {
		return "", fmt.Errorf("script: parse %q: %w", file, err)
	} else {
		return string(src), nil
	}
}

func scriptEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case embedNATSVar, natsURLVar, natsClusterVar, "SMT_HOST", "SMT_REGISTRY_FILE":
		default:
			env = append(env, entry)
		}
	}
	return append(env, embedNATSVar+"=1", "SMT_HOST=127.0.0.1", natsClusterVar+"=script_"+newUUID())
}

func scriptTimeout() time.Duration {
	// Each startup/evaluation phase is already bounded; cap their aggregate so
	// a coordinator that fails to shut down cannot keep the script runner alive.
	total := 10 * time.Second
	for _, budget := range []time.Duration{natsStartupTimeout(), spawnTimeout(), evalTimeout()} {
		if budget >= 24*time.Hour-total {
			return 24 * time.Hour
		} else {
			total += budget
		}
	}
	return total
}

// runTests runs each test file and prints a summary. Returns exit code.
func runTests(args []string) int {
	files := args
	if len(files) == 0 {
		a, _ := filepath.Glob("*_test.smt")
		b, _ := filepath.Glob("*Test.smt")
		files = append(a, b...)
		if len(files) == 0 {
			fmt.Fprintln(os.Stderr, "no test files found (*_test.smt or *Test.smt)")
			return 1
		}
	}

	passed, failed := 0, 0
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".smt")
		code := runScript(f, 0)
		if code == 0 {
			fmt.Printf("ok   %s\n", name)
			passed++
		} else {
			fmt.Printf("FAIL %s\n", name)
			failed++
		}
	}

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	} else {
		return 0
	}
}
