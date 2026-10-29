//go:build windows

package main

import (
 "os"
 "os/exec"
)

func configureJobGroup(command *exec.Cmd) {}
func signalJobGroup(command *exec.Cmd,signal os.Signal) error {
 if command.Process==nil { return os.ErrProcessDone } else { return command.Process.Signal(signal) }
}

func configureTerminalIO(terminal *os.File) error { return os.ErrInvalid }
func resizeTerminal(terminal *os.File,columns,rows uint16) error { return os.ErrInvalid }
