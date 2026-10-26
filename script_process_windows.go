//go:build windows

package main

import (
	"os/exec"
	"time"
)

func configureScriptProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second
}

func cleanupScriptProcess(cmd *exec.Cmd) {}
