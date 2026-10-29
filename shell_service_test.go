package main

import (
 "bytes"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func shellTestRuntime(t *testing.T) string {
 t.Helper()
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 return startMCPTestCoordinator(t,broker.ClientURL(),t.TempDir())
}

func TestShellFilesAndSessionState(t *testing.T) {
 base:=shellTestRuntime(t)
 directory:=filepath.Join(t.TempDir(),"a directory with spaces")
 sourceTestEval(t,base,"shell := Shell new. shell mkdir: "+smalltalkString(directory)+". shell cd: "+smalltalkString(directory))
 sourceTestEval(t,base,"shell write: 'hello ; world.txt' text: 'red cat'. shell append: 'hello ; world.txt' text: ' and blue cat'. shell mkdir: 'nested folder'. shell write: 'nested folder/second.txt' text: 'second'")
 checks:=map[string]string{
  "shell cat: 'hello ; world.txt'":"red cat and blue cat",
  "shell ls size":"2",
  "(shell find: '*.txt') size":"2",
  "(shell find: '*.txt') first":"hello ; world.txt",
  "(shell grep: 'blue' in: 'hello ; world.txt') first":"red cat and blue cat",
  "shell env: 'SERVICETALK_SHELL_SESSION' put: 'literal value'. shell env: 'SERVICETALK_SHELL_SESSION'":"literal value",
  "shell unsetEnv: 'SERVICETALK_SHELL_SESSION'. (shell env: 'SERVICETALK_SHELL_SESSION') isNil":"true",
  "shell pwd":directory,
 }
 for code,want:=range checks {
  if got:=sourceTestEval(t,base,code);got!=want { t.Fatalf("%s: got %q want %q",code,got,want) } else {}
 }
 for _,code:=range []string{"shell cat: 'missing'","shell cd: 'missing'","shell ls: 'missing'","shell find: '['","shell write: 'missing/file' text: 'x'","shell env: 'BAD=NAME' put: 'value'"} {
  result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
  if !result.IsError { t.Fatalf("missing error for %s: %+v",code,result) } else {}
 }
 if got:=sourceTestEval(t,base,"shell pwd");got!=directory { t.Fatalf("failed cd changed directory: %q",got) } else {}
 if got:=sourceTestEval(t,base,"other := Shell new. (other env: 'SERVICETALK_SHELL_SESSION') isNil");got!="true" { t.Fatalf("environment leaked to another session: %s",got) } else {}
}

func TestShellChildProcess(t *testing.T) {
 if os.Getenv("SERVICETALK_SHELL_HELPER")!="1" { return } else {}
 input,err:=io.ReadAll(os.Stdin)
 if err!=nil { os.Exit(9) } else {}
 directory,err:=os.Getwd()
 if err!=nil { os.Exit(8) } else {}
 fmt.Fprint(os.Stdout,"cwd="+directory+"\nenv="+os.Getenv("SERVICETALK_SHELL_VALUE")+"\ninput="+string(input)+"\narg="+os.Args[len(os.Args)-1])
 fmt.Fprint(os.Stderr,"diagnostic line")
 os.Exit(7)
}

func TestShellProcessStreamsInputAndEnvironment(t *testing.T) {
 base:=shellTestRuntime(t)
 directory:=filepath.Join(t.TempDir(),"process directory")
 executable,err:=os.Executable()
 if err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,"shell := Shell new. shell mkdir: "+smalltalkString(directory)+". shell cd: "+smalltalkString(directory)+". shell env: 'SERVICETALK_SHELL_HELPER' put: '1'. shell env: 'SERVICETALK_SHELL_VALUE' put: 'persisted value'")
 command:="shell run: "+smalltalkString(executable)+" args: #('-test.run=^TestShellChildProcess$' '--' 'spaces ; $literal') input: 'hello stdin'"
 sourceTestEval(t,base,"job := "+command)
 want:="cwd="+directory+"\nenv=persisted value\ninput=hello stdin\narg=spaces ; $literal"
 checks:=map[string]string{"job stdout":want,"job stderr":"diagnostic line","job exitCode":"7","job status":"exited","job error = ''":"true","job directory":directory,"shell jobs size":"1","shell lastProcess = job":"true"}
 for code,expected:=range checks {
  if got:=sourceTestEval(t,base,code);got!=expected { t.Fatalf("%s: got %q want %q",code,got,expected) } else {}
 }
 combined:=sourceTestEval(t,base,"job output")
 if !strings.Contains(combined,want) || !strings.Contains(combined,"diagnostic line") { t.Fatalf("combined output: %q",combined) } else {}
 if got:=sourceTestEval(t,base,"job inspect. job exitCode");!strings.Contains(got,"7") { t.Fatalf("inspect did not return: %q",got) } else {}
 for _,code:=range []string{"job input: 'changed'","job environment: Dictionary new","job start: 'again'"} {
  result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
  if !result.IsError { t.Fatalf("accepted reuse: %s",code) } else {}
 }
 sourceTestEval(t,base,"bad := shell run: 'servicetalk-command-that-does-not-exist' args: #()")
 for code,want:=range map[string]string{"bad status":"error","bad exitCode":"-1","bad wait":"-1"} {
  if got:=sourceTestEval(t,base,code);got!=want { t.Fatalf("%s: %q",code,got) } else {}
 }
 if got:=sourceTestEval(t,base,"bad error");!strings.Contains(got,"servicetalk-command-that-does-not-exist") || !strings.Contains(got,directory) { t.Fatalf("startup error lacks context: %q",got) } else {}
}

func TestProcessSessionPathOverride(t *testing.T) {
 directory:=t.TempDir()
 executable,err:=os.Executable()
 if err!=nil { t.Fatal(err) } else {}
 name:=filepath.Base(executable)
 command:=processCommand(name,nil,directory,[]string{"PATH="+filepath.Dir(executable)})
 if command.Err!=nil || command.Path!=executable { t.Fatalf("PATH override: %+v",command) } else {}
 missing:=processCommand(name,nil,directory,[]string{"PATH="+directory})
 if missing.Err==nil { t.Fatal("ignored overridden PATH") } else {}
}

func TestShellClassPathPrefersCurrentAndExplicitClasses(t *testing.T) {
 originalPath,originalUser:=classPath,userClassPath
 t.Cleanup(func(){classPath=originalPath;userClassPath=originalUser})
 t.Setenv("SMT_PATH","")
 setShellClassPath(nil)
 current,err:=LoadClass("Shell")
 if err!=nil { t.Fatal(err) } else if _,ok:=current.Methods["run:args:"];!ok { t.Fatal("default Shell is the obsolete example") } else {}
 directory:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(directory,"Shell.st"),[]byte("class Shell [ customMarker [ ^ 42 ] ]"),0600);err!=nil { t.Fatal(err) } else {}
 setShellClassPath([]string{directory})
 explicit,err:=LoadClass("Shell")
 if err!=nil { t.Fatal(err) } else if _,ok:=explicit.Methods["customMarker"];!ok { t.Fatal("explicit Shell override lost precedence") } else {}
 if len(userClassPath)!=len(classPath) { t.Fatal("child objects lose shell fallback paths") } else {}
}

func TestProcessRejectsMalformedStarts(t *testing.T) {
 base:=shellTestRuntime(t)
 for _,code:=range []string{"Process new start: 'go' args: 42","Process new start: 'go' args: #('version' 42)","Process new start: 'go' args: Dictionary new","Process new start: 42","Process new start: ''","Process new start: 'go' args: #('version') cwd: nil"} {
  result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
  if !result.IsError { t.Fatalf("malformed start executed: %s",code) } else {}
 }
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Process new"})
 if created.Error!="" { t.Fatal(created.Error) } else {}
 for _,method:=range []string{"start:","start:cwd:","start:args:","start:args:cwd:"} {
  result:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:created.Result.ObjID,Method:method})
  if !result.IsError { t.Fatalf("missing arguments accepted for %s",method) } else {}
 }
}

func TestProcessOutputExactLimitReportsTruncation(t *testing.T) {
 state:=&processState{stdout:&bytes.Buffer{},standardOutput:&bytes.Buffer{}}
 writer:=&processWriter{st:state,stream:state.standardOutput}
 writer.Write(bytes.Repeat([]byte("x"),processOutputCap))
 writer.Write([]byte("overflow"))
 writer.Write([]byte("more overflow"))
 for _,buffer:=range []*bytes.Buffer{state.stdout,state.standardOutput} {
  if strings.Count(buffer.String(),"…<truncated>")!=1 { t.Fatal("missing or repeated truncation marker at exact output limit") } else {}
 }
}

func TestShellHomeUsesSessionEnvironment(t *testing.T) {
 base:=shellTestRuntime(t)
 first,second:=t.TempDir(),t.TempDir()
 sourceTestEval(t,base,"shell := Shell new. other := Shell new. shell env: 'HOME' put: "+smalltalkString(first)+". other env: 'HOME' put: "+smalltalkString(second))
 for code,want:=range map[string]string{"shell home":first,"shell cd: '~'":first,"other home":second,"other cd: '~'":second,"shell resolvePath: '~/notes'":filepath.Join(first,"notes")} {
  if got:=sourceTestEval(t,base,code);got!=want { t.Fatalf("%s got %q want %q",code,got,want) } else {}
 }
 sourceTestEval(t,base,"shell unsetEnv: 'HOME'. shell unsetEnv: 'USERPROFILE'")
 missing:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"shell cd: '~'"})
 if !missing.IsError || !strings.Contains(missing.Content[0].Text,"home directory is unset") { t.Fatalf("unset home: %+v",missing) } else {}
 if got:=sourceTestEval(t,base,"shell pwd");got!=first { t.Fatalf("failed home navigation changed cwd: %q",got) } else {}
}
