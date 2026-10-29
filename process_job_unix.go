//go:build !windows

package main

import (
 "os"
 "os/exec"
 "syscall"

 "golang.org/x/sys/unix"
)

func configureJobGroup(command *exec.Cmd) { command.SysProcAttr=&syscall.SysProcAttr{Setpgid:true} }
func signalJobGroup(command *exec.Cmd,signal os.Signal) error {
 if command.Process==nil { return os.ErrProcessDone } else {}
 native,ok:=signal.(syscall.Signal)
 if !ok { return command.Process.Signal(signal) } else { return syscall.Kill(-command.Process.Pid,native) }
}

func configureTerminalIO(terminal *os.File) error {
 raw,err:=terminal.SyscallConn()
 if err!=nil { return err } else {}
 var configureErr error
 err=raw.Control(func(fd uintptr){configureErr=syscall.SetNonblock(int(fd),true)})
 if err!=nil { return err } else { return configureErr }
}

func resizeTerminal(terminal *os.File,columns,rows uint16) error {
 raw,err:=terminal.SyscallConn()
 if err!=nil { return err } else {}
 var resizeErr error
 err=raw.Control(func(fd uintptr){resizeErr=unix.IoctlSetWinsize(int(fd),unix.TIOCSWINSZ,&unix.Winsize{Col:columns,Row:rows})})
 if err!=nil { return err } else { return resizeErr }
}
