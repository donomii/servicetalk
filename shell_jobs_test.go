package main

import (
 "bufio"
 "bytes"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
 "time"

 "golang.org/x/term"
)

func TestShellTerminalChild(t *testing.T) {
 if os.Getenv("SERVICETALK_TERMINAL_HELPER")!="1" { return } else {}
 if !term.IsTerminal(int(os.Stdin.Fd())) { fmt.Print("NOT_A_TERMINAL");os.Exit(8) } else {}
 columns,rows,_:=term.GetSize(int(os.Stdin.Fd()));fmt.Printf("READY %dx%d\n",columns,rows)
 scanner:=bufio.NewScanner(os.Stdin)
 for scanner.Scan() {
  switch scanner.Text() {
  case "quit":fmt.Println("GOODBYE");os.Exit(3)
  case "size":columns,rows,_=term.GetSize(int(os.Stdin.Fd()));fmt.Printf("SIZE %dx%d\n",columns,rows)
  default:fmt.Println("ECHO "+scanner.Text())
  }
 }
 os.Exit(0)
}

func TestShellPipelineChild(t *testing.T) {
 if os.Getenv("SERVICETALK_PIPELINE_HELPER")!="1" { return } else {}
 mode:=os.Args[len(os.Args)-1]
 switch mode {
 case "produce":
  block:=make([]byte,65536);for i:=range block { block[i]=byte(i) }
  for i:=0;i<40;i++ { if _,err:=os.Stdout.Write(block);err!=nil { os.Exit(9) } else {} };os.Exit(0)
 case "copy":_,err:=io.Copy(os.Stdout,os.Stdin);if err!=nil { os.Exit(8) } else { os.Exit(0) }
 case "hash":hash:=sha256.New();count,err:=io.Copy(hash,os.Stdin);if err!=nil { os.Exit(7) } else { fmt.Printf("%d %x",count,hash.Sum(nil));os.Exit(0) }
 case "fail":fmt.Fprint(os.Stderr,"stage failed");os.Exit(5)
 case "wait":time.Sleep(20*time.Second);os.Exit(0)
 }
 os.Exit(6)
}

func terminalAwaitOutput(t *testing.T,base,id,want string) string {
 t.Helper()
 deadline:=time.Now().Add(5*time.Second)
 for time.Now().Before(deadline) {
  value:=controlTestCall(t,base,id,"output")
  if strings.Contains(value.Str,want) { return value.Str } else { time.Sleep(10*time.Millisecond) }
 }
 t.Fatalf("terminal did not produce %q",want)
 return ""
}

func TestShellTerminalInteractiveResizeAndExit(t *testing.T) {
 if runtime.GOOS=="windows" { t.Skip("PTY adapter is Unix-only") } else {}
 base:=shellTestRuntime(t)
 executable,err:=os.Executable();if err!=nil { t.Fatal(err) } else {}
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"shell := Shell new. shell env: 'SERVICETALK_TERMINAL_HELPER' put: '1'. terminal := shell terminal: "+smalltalkString(executable)+" args: #('-test.run=^TestShellTerminalChild$'). terminal"})
 if created.Error!="" { t.Fatal(created.Error) } else {}
 terminal:=created.Result.ObjID
 t.Cleanup(func(){controlTestCall(t,base,terminal,"signal:",StrVal("KILL"))})
 terminalAwaitOutput(t,base,terminal,"READY 80x24")
 controlTestCall(t,base,terminal,"resizeColumns:rows:",NumVal(100),NumVal(35))
 controlTestCall(t,base,terminal,"sendLine:",StrVal("size"))
 terminalAwaitOutput(t,base,terminal,"SIZE 100x35")
 controlTestCall(t,base,terminal,"sendLine:",StrVal("hello ; literal"))
 terminalAwaitOutput(t,base,terminal,"ECHO hello ; literal")
 first:=controlTestCall(t,base,terminal,"read")
 if !strings.Contains(first.Str,"READY") || controlTestCall(t,base,terminal,"read").Str!="" { t.Fatalf("read did not drain unread output: %+v",first) } else {}
 controlTestCall(t,base,terminal,"sendLine:",StrVal("quit"))
 if code:=controlTestCall(t,base,terminal,"wait");code.Num!=3 { t.Fatalf("exit: %+v",code) } else {}
 terminalAwaitOutput(t,base,terminal,"GOODBYE")
 if controlTestCall(t,base,terminal,"status").Str!="exited" { t.Fatal("terminal did not exit") } else {}
}

func pipelineTestStages(executable string,modes ...string) string {
 parts:=[]string{}
 for _,mode:=range modes { parts=append(parts,"{"+smalltalkString(executable)+". '-test.run=^TestShellPipelineChild$'. '--'. "+smalltalkString(mode)+"}") }
 return "{"+strings.Join(parts,". ")+"}"
}

func TestShellPipelineStreamsLargeBinaryAndReportsEveryExit(t *testing.T) {
 base:=shellTestRuntime(t)
 executable,err:=os.Executable();if err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,"shell := Shell new. shell env: 'SERVICETALK_PIPELINE_HELPER' put: '1'")
 stages:=pipelineTestStages(executable,"produce","copy","hash")
 sourceTestEval(t,base,"pipeline := shell pipeline: "+stages+". pipeline wait")
 content:=make([]byte,40*65536);for i:=range content { content[i]=byte(i) }
 expected:=fmt.Sprintf("%d %x",len(content),sha256.Sum256(content))
 if got:=sourceTestEval(t,base,"pipeline stdout");got!=expected { t.Fatalf("pipeline bytes truncated: got %q want %q",got,expected) } else {}
 if got:=sourceTestEval(t,base,"pipeline exitCodes printString");got!="0\n0\n0" { t.Fatalf("stage exits: %q",got) } else {}
 destination:=filepath.Join(t.TempDir(),"binary output.bin")
 sourceTestEval(t,base,"pipeline := shell pipeline: "+pipelineTestStages(executable,"produce","copy")+" outputFile: "+smalltalkString(destination)+". pipeline wait")
 written,err:=os.ReadFile(destination);if err!=nil { t.Fatal(err) } else if !bytes.Equal(written,content) { t.Fatal("pipeline output file did not preserve binary bytes") } else {}
 sourceTestEval(t,base,"pipeline := shell pipeline: "+pipelineTestStages(executable,"fail","copy")+". pipeline wait")
 if got:=sourceTestEval(t,base,"pipeline exitCode");got!="5" { t.Fatalf("upstream failure hidden: %s",got) } else {}
 if got:=sourceTestEval(t,base,"pipeline stderr");got!="stage failed" { t.Fatalf("stderr: %q",got) } else {}
 sourceTestEval(t,base,"pipeline := shell pipeline: {#('servicetalk-missing-command'). #('also-missing')}. pipeline wait")
 if got:=sourceTestEval(t,base,"pipeline error");!strings.Contains(got,"stage 1") { t.Fatalf("startup diagnostic: %q",got) } else {}
 sourceTestEval(t,base,"pipeline := shell pipeline: "+pipelineTestStages(executable,"copy","hash")+" input: 'literal input' outputFile: ''. pipeline wait")
 if got:=sourceTestEval(t,base,"pipeline stdout");got!=fmt.Sprintf("13 %x",sha256.Sum256([]byte("literal input"))) { t.Fatalf("pipeline stdin: %q",got) } else {}
 failedStages:=strings.TrimSuffix(pipelineTestStages(executable,"wait"),"}")+". #('servicetalk-missing-command')}"
 started:=time.Now()
 sourceTestEval(t,base,"pipeline := shell pipeline: "+failedStages+". pipeline wait")
 if got:=sourceTestEval(t,base,"pipeline error");!strings.Contains(got,"stage 2") || time.Since(started)>3*time.Second { t.Fatalf("failed stage cleanup: %q",got) } else {}
}

func TestShellCompletionUsesSessionPathAndHome(t *testing.T) {
 base:=shellTestRuntime(t)
 directory:=t.TempDir();bin:=filepath.Join(directory,"bin")
 if err:=os.MkdirAll(bin,0755);err!=nil { t.Fatal(err) } else {}
 name:="demo-command";if runtime.GOOS=="windows" { name+=".exe" } else {}
 if err:=os.WriteFile(filepath.Join(bin,name),[]byte("not executed"),0755);err!=nil { t.Fatal(err) } else {}
 if err:=os.WriteFile(filepath.Join(bin,"demo-not-executable"),nil,0644);err!=nil { t.Fatal(err) } else {}
 if err:=os.Mkdir(filepath.Join(directory,"space folder"),0755);err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,"shell := Shell new. shell cd: "+smalltalkString(directory)+". shell env: 'PATH' put: "+smalltalkString(bin)+". shell env: 'HOME' put: "+smalltalkString(directory))
 if got:=sourceTestEval(t,base,"(shell completeCommand: 'demo-') first");got!=name { t.Fatalf("commands: %q",got) } else {}
 if got:=sourceTestEval(t,base,"(shell completePath: '~/spa') first");got!="~/space folder"+string(filepath.Separator) { t.Fatalf("home path completion: %q",got) } else {}
}

func TestTerminalReadersKeepIndependentByteOffsets(t *testing.T) {
 state:=&processState{started:true}
 read:=func(offset int64) (TerminalReadResponse,[]byte) {
  var response TerminalReadResponse
  if err:=json.Unmarshal([]byte(terminalReadFrom(state,offset).Str),&response);err!=nil { t.Fatal(err) } else {}
  raw,err:=base64.StdEncoding.DecodeString(response.Data);if err!=nil { t.Fatal(err) } else {}
  return response,raw
 }
 encoded:=[]byte("猫🙂")
 appendTerminalOutput(state,encoded[:2])
 first,firstBytes:=read(0)
 appendTerminalOutput(state,encoded[2:])
 _,secondBytes:=read(first.NextOffset)
 if !bytes.Equal(append(firstBytes,secondBytes...),encoded) { t.Fatal("split UTF-8 bytes were changed") } else {}
 _,otherReader:=read(0)
 if !bytes.Equal(otherReader,encoded) { t.Fatal("first reader consumed another reader's output") } else {}
 appendTerminalOutput(state,bytes.Repeat([]byte("x"),processOutputCap+17))
 trimmed,available:=read(0)
 if trimmed.Dropped!=int64(len(encoded)+17) || len(available)!=terminalReadBatchSize || trimmed.OldestOffset!=trimmed.Dropped || trimmed.NextOffset!=trimmed.OldestOffset+terminalReadBatchSize { t.Fatalf("ring offsets: %+v",trimmed) } else {}
 total:=int64(len(encoded)+processOutputCap+17)
 for trimmed.NextOffset<total {
  next,chunk:=read(trimmed.NextOffset)
  if next.NextOffset<=trimmed.NextOffset || len(chunk)>terminalReadBatchSize || next.Dropped!=0 { t.Fatalf("invalid batch: %+v",next) } else {}
  available=append(available,chunk...);trimmed=next
 }
 if len(available)!=processOutputCap || trimmed.NextOffset!=total { t.Fatal("cursor did not deliver every retained byte") } else {}
}

func TestShellTerminalRunsInteractiveShell(t *testing.T) {
 if runtime.GOOS=="windows" { t.Skip("PTY adapter is Unix-only") } else {}
 program,err:=exec.LookPath("sh");if err!=nil { t.Skip("sh unavailable") } else {}
 base:=shellTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"terminal := Shell new terminal: "+smalltalkString(program)+" args: #('-i'). terminal"})
 if created.Error!="" { t.Fatal(created.Error) } else {}
 terminal:=created.Result.ObjID
 t.Cleanup(func(){controlTestCall(t,base,terminal,"signal:",StrVal("KILL"))})
 controlTestCall(t,base,terminal,"sendLine:",StrVal("printf 'PTY_TASK:%s\\n' \"$((6 * 7))\""))
 terminalAwaitOutput(t,base,terminal,"PTY_TASK:42")
 controlTestCall(t,base,terminal,"sendLine:",StrVal("exit 0"))
 if code:=controlTestCall(t,base,terminal,"wait");code.Num!=0 { t.Fatalf("shell exit: %+v",code) } else {}
}

func TestShellJobsStopWhileWaiting(t *testing.T) {
 base:=shellTestRuntime(t)
 executable,err:=os.Executable();if err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,"shell := Shell new. shell env: 'SERVICETALK_PIPELINE_HELPER' put: '1'. shell env: 'SERVICETALK_TERMINAL_HELPER' put: '1'")
 cases:=[]string{"shell pipeline: "+pipelineTestStages(executable,"wait","copy")}
 if runtime.GOOS!="windows" { cases=append(cases,"shell terminal: "+smalltalkString(executable)+" args: #('-test.run=^TestShellTerminalChild$')") } else {}
 for _,source:=range cases {
  created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:source})
  if created.Error!="" { t.Fatal(created.Error) } else {}
  done:=make(chan InteractionResult,1)
  go func(){done<-testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:created.Result.ObjID,Method:"wait"})}()
  time.Sleep(30*time.Millisecond)
  if status:=controlTestCall(t,base,created.Result.ObjID,"status");status.Str!="running" { t.Fatalf("job status while waiting: %+v",status) } else {}
  controlTestCall(t,base,created.Result.ObjID,"inspectorViews")
  if stopped:=controlTestCall(t,base,created.Result.ObjID,"stop");!stopped.Bool { t.Fatalf("stop did not signal job: %+v",stopped) } else {}
  select { case result:=<-done:if result.Error!="" { t.Fatal(result.Error) } else {};case <-time.After(3*time.Second):t.Fatal("job wait did not return after stop") }
 }
}

func TestShellQuotedCompletionKeepsSpacesAndQuotes(t *testing.T) {
 line:="cat: 'some fi"
 opening,fragment,ok:=shellQuotedFragment(line)
 if !ok || fragment!="some fi" { t.Fatalf("quoted fragment %q",fragment) } else {}
 result,_,changed:=shellQuotedCompletion(line,len(line),opening,fragment,[]string{"some file's.txt"})
 if !changed || result!="cat: 'some file''s.txt' " { t.Fatalf("quoted completion: %q",result) } else {}
 opening,fragment,ok=shellQuotedFragment("cat: 'some file''s")
 if !ok || opening!=5 || fragment!="some file's" { t.Fatalf("escaped quote fragment: %q",fragment) } else {}
}
