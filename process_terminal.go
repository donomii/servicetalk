package main

import (
 "encoding/base64"
 "encoding/json"
 "fmt"
 "math"
 "os"
 "strings"
 "sync/atomic"
 "time"

 "github.com/creack/pty"
)

const terminalReadBatchSize = 64*1024

func (obj *ObjectServer) processTerminalMethod(name string,args []Value) Value {
 state:=obj.processState()
 switch name {
 case "terminalReadFrom:":
  if len(args)!=1 || args[0].Kind!="number" || args[0].Num<0 || args[0].Num>1<<53 || math.Trunc(args[0].Num)!=args[0].Num { panic(signalError{msg:"Terminal read offset must be a nonnegative integer byte offset"}) } else {}
  return terminalReadFrom(state,int64(args[0].Num))
 case "terminalStart:args:cwd:columns:rows:":
  if len(args)!=5 || args[0].Kind!="string" || args[0].Str=="" || args[2].Kind!="string" { panic(signalError{msg:"Terminal start requires program, argument Array, directory, columns and rows"}) } else {}
  columns,rows:=terminalSize(args[3],args[4])
  obj.processStartTerminal(args[0].Str,obj.arrayToStrings(args[1]),args[2].Str,columns,rows)
  return ObjVal(obj.id)
 case "terminalResizeColumns:rows:":
  if len(args)!=2 { panic(signalError{msg:"Terminal resize requires columns and rows"}) } else {}
  columns,rows:=terminalSize(args[0],args[1])
  terminal:=processTerminalFile(state)
  if err:=resizeTerminal(terminal,columns,rows);err!=nil { panic(signalError{msg:"Resize terminal: "+err.Error()}) } else { return ObjVal(obj.id) }
 case "terminalWrite:":
  if len(args)!=1 || args[0].Kind!="string" { panic(signalError{msg:"Terminal write requires text"}) } else {}
  terminal:=processTerminalFile(state)
  if err:=terminal.SetWriteDeadline(time.Now().Add(time.Second));err!=nil { panic(signalError{msg:"Prepare terminal input: "+err.Error()}) } else {}
  count,err:=terminal.Write([]byte(args[0].Str))
  if err!=nil { panic(signalError{msg:fmt.Sprintf("Terminal wrote %d of %d input bytes: %v",count,len(args[0].Str),err)}) } else { return NumVal(float64(count)) }
 default:
  state.mu.Lock();defer state.mu.Unlock()
  switch name {
  case "terminalRead": result:=string(state.terminalPending);state.terminalPending=nil;return StrVal(result)
  case "terminalOutput": return StrVal(string(state.terminalRecent))
  case "terminalDropped": return NumVal(float64(state.terminalDropped))
  default:return Nil
  }
 }
}

func terminalSize(columns,rows Value) (uint16,uint16) {
 for _,value:=range []Value{columns,rows} {
  if value.Kind!="number" || value.Num<1 || value.Num>65535 || math.Trunc(value.Num)!=value.Num { panic(signalError{msg:"Terminal dimensions must be integers from 1 to 65535"}) } else {}
 }
 return uint16(columns.Num),uint16(rows.Num)
}

func processTerminalFile(state *processState) *os.File {
 state.mu.Lock();defer state.mu.Unlock()
 if state.terminal==nil || atomic.LoadInt32(&state.finished)!=0 { panic(signalError{msg:"Terminal is not running"}) } else { return state.terminal }
}

func (obj *ObjectServer) processStartTerminal(program string,arguments []string,directory string,columns,rows uint16) {
 state:=obj.processState()
 environment,_:=processBeginJob(state,strings.Join(append([]string{program},arguments...)," "),directory)
 command:=processCommand(program,arguments,directory,environment);command.Dir=directory
 terminal,err:=pty.StartWithSize(command,&pty.Winsize{Cols:columns,Rows:rows})
 if err!=nil { processFinishJob(state,-1,fmt.Errorf("start terminal %q in %q: %w",program,directory,err));return } else {}
 if err=configureTerminalIO(terminal);err!=nil { signalJobGroup(command,os.Kill);command.Wait();terminal.Close();processFinishJob(state,-1,fmt.Errorf("configure terminal %q: %w",program,err));return } else {}
 state.mu.Lock();state.cmd=command;state.terminal=terminal;state.mu.Unlock()
 drained:=make(chan struct{})
 go readTerminalOutput(state,terminal,drained)
 go func(){
  code,err:=processWaitCode(command)
  select { case <-drained:case <-time.After(time.Second):terminal.Close();<-drained }
  terminal.Close()
  processFinishJob(state,code,err)
 }()
}

func readTerminalOutput(state *processState,terminal *os.File,done chan struct{}) {
 defer close(done)
 buffer:=make([]byte,32*1024)
 for {
  count,err:=terminal.Read(buffer)
  if count>0 { appendTerminalOutput(state,buffer[:count]) } else {}
  if err!=nil { return } else {}
 }
}

func appendTerminalOutput(state *processState,data []byte) {
 state.mu.Lock();defer state.mu.Unlock()
 state.terminalTotal+=int64(len(data))
 if state.stdout!=nil { processAppendOutput(state.stdout,data) } else {}
 if state.standardOutput!=nil { processAppendOutput(state.standardOutput,data) } else {}
 state.terminalPending=append(state.terminalPending,data...)
 if excess:=len(state.terminalPending)-processOutputCap;excess>0 { state.terminalPending=append([]byte{},state.terminalPending[excess:]...);state.terminalDropped+=int64(excess) } else {}
 state.terminalRecent=append(state.terminalRecent,data...)
 if excess:=len(state.terminalRecent)-processOutputCap;excess>0 { state.terminalRecent=append([]byte{},state.terminalRecent[excess:]...) } else {}
}

func terminalReadFrom(state *processState,offset int64) Value {
 state.mu.Lock();defer state.mu.Unlock()
 oldest:=state.terminalTotal-int64(len(state.terminalRecent))
 response:=TerminalReadResponse{NextOffset:state.terminalTotal,OldestOffset:oldest,Status:"starting"}
 if offset>state.terminalTotal { panic(signalError{msg:fmt.Sprintf("Terminal read offset %d exceeds output length %d",offset,state.terminalTotal)}) } else if offset<oldest { response.Dropped=oldest-offset;offset=oldest } else {}
 end:=state.terminalTotal
 if end-offset>terminalReadBatchSize { end=offset+terminalReadBatchSize } else {}
 response.NextOffset=end
 response.Data=base64.StdEncoding.EncodeToString(state.terminalRecent[offset-oldest:end-oldest])
 if state.started { response.Status="running" } else {}
 if atomic.LoadInt32(&state.finished)!=0 { response.ExitCode=&state.exitCode;if state.exitErr!="" { response.Status="error" } else { response.Status="exited" } } else {}
 encoded,err:=json.Marshal(response)
 if err!=nil { panic(signalError{msg:"Read terminal output: "+err.Error()}) } else { return StrVal(string(encoded)) }
}
