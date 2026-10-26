//go:build windows

package main

import (
	"os"
	"strings"
)

func shellCommand(cmdLine string) (string, []string) {
	return "cmd.exe", []string{"/C", cmdLine}
}

func signalFromName(name string) os.Signal {
	switch strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(name, "SIG"), "sig")) {
	case "KILL", "TERM":
		return os.Kill
	case "INT":
		return os.Interrupt
	default:
		return nil
	}
}
