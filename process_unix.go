//go:build !windows

package main

import (
	"os"
	"strings"
	"syscall"
)

func shellCommand(cmdLine string) (string, []string) {
	return "/bin/sh", []string{"-c", cmdLine}
}

func signalFromName(name string) os.Signal {
	switch strings.ToUpper(strings.TrimPrefix(strings.TrimPrefix(name, "SIG"), "sig")) {
	case "TERM":
		return syscall.SIGTERM
	case "KILL":
		return syscall.SIGKILL
	case "INT":
		return syscall.SIGINT
	case "HUP":
		return syscall.SIGHUP
	case "QUIT":
		return syscall.SIGQUIT
	case "USR1":
		return syscall.SIGUSR1
	case "USR2":
		return syscall.SIGUSR2
	default:
		return nil
	}
}
