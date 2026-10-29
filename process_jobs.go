package main

import (
 "fmt"
 "os/exec"
 "sync/atomic"
)

func processBeginJob(state *processState,command,directory string) ([]string,string) {
 state.mu.Lock()
 defer state.mu.Unlock()
 if state.started { panic(signalError{msg:"Process already started; create a new Process for another command"}) } else {}
 state.started=true;state.command=command;state.directory=directory
 environment:=state.environment
 if environment!=nil { environment=append([]string{},environment...) } else {}
 return environment,state.input
}

func processFinishJob(state *processState,code int,err error) {
 state.mu.Lock()
 defer state.mu.Unlock()
 state.exitCode=code
 if err!=nil { state.exitErr=err.Error() } else {}
 atomic.StoreInt32(&state.finished,1)
 close(state.done)
}

func processWaitCode(command *exec.Cmd) (int,error) {
 err:=command.Wait()
 if err==nil { return 0,nil } else if exited,ok:=err.(*exec.ExitError);ok { return exited.ExitCode(),nil } else { return -1,fmt.Errorf("wait for %q: %w",command.Path,err) }
}

func processSetCommands(state *processState,commands []*exec.Cmd) {
 state.mu.Lock()
 defer state.mu.Unlock()
 state.pipeline=commands
 if len(commands)>0 { state.cmd=commands[len(commands)-1] } else {}
 state.pipelineCodes=make([]int,len(commands))
 for i:=range state.pipelineCodes { state.pipelineCodes[i]=-1 }
}
