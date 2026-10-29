package main

import (
 "bytes"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
)

func processValidateStart(name string,args []Value) {
 want:=0
 switch name {
 case "start:": want=1
 case "start:cwd:","start:args:": want=2
 case "start:args:cwd:": want=3
 default: return
 }
 if len(args)!=want { panic(signalError{msg:fmt.Sprintf("Process %s expects %d arguments; received %d",name,want,len(args))}) } else {}
 for i,value:=range args {
  if i==1 && strings.Contains(name,"args:") { continue } else if value.Kind!="string" { panic(signalError{msg:fmt.Sprintf("Process %s argument %d must be a string; received %s",name,i,value.Kind)}) } else {}
 }
 if args[0].Str=="" { panic(signalError{msg:"Process start requires a nonempty command"}) } else {}
}

func (obj *ObjectServer) processSessionMethod(name string,args []Value) Value {
 switch name {
 case "inspectorMetadata": return StrVal(`{"properties":[{"name":"Status","getter":"status","type":"string"},{"name":"Exit code","getter":"exitCode","type":"number"},{"name":"Command","getter":"command","type":"string"},{"name":"Directory","getter":"directory","type":"string"}],"methods":{"input:":{"arguments":[{"name":"text","type":"string"}]},"environment:":{"arguments":[{"name":"overrides","type":"objid"}]},"signal:":{"arguments":[{"name":"signal","type":"string"}],"returns":"bool"}}}`)
 case "inspectorViews": return StrVal(`{"views":[{"id":"process","title":"Process","body":{"kind":"column","children":[{"kind":"code","label":"Command","getter":"command"},{"kind":"row","children":[{"kind":"text","label":"Status","getter":"status"},{"kind":"text","label":"Exit code","getter":"exitCode"}]},{"kind":"value","label":"Directory","getter":"directory"},{"kind":"code","label":"Standard output","getter":"stdout"},{"kind":"code","label":"Standard error","getter":"stderr"},{"kind":"value","label":"Start error","getter":"error"},{"kind":"row","children":[{"kind":"action","text":"Wait for exit","method":"wait"},{"kind":"action","text":"Stop","method":"signal:","args":[{"kind":"string","str":"TERM"}]}]}]}}]}`)
 case "environment:":
  if len(args)!=1 || args[0].Kind!="objid" { panic(signalError{msg:"Process environment: requires a Dictionary of string overrides; nil values unset variables"}) } else {}
  environment:=obj.processEnvironment(args[0])
  st:=obj.processState();st.mu.Lock();defer st.mu.Unlock()
  if st.started { panic(signalError{msg:"Process environment must be configured before start"}) } else { st.environment=environment;return ObjVal(obj.id) }
 default:
  st:=obj.processState();st.mu.Lock();defer st.mu.Unlock()
  switch name {
  case "input:":
   if len(args)!=1 || args[0].Kind!="string" { panic(signalError{msg:"Process input: requires a string"}) } else if st.started { panic(signalError{msg:"Process input must be configured before start"}) } else { st.input=args[0].Str;return ObjVal(obj.id) }
  case "stdout": return StrVal(st.standardOutput.String())
  case "stderr": return StrVal(st.errorOutput.String())
  case "error": return StrVal(st.exitErr)
  case "directory": return StrVal(st.directory)
  default: return Nil
  }
 }
}

func (obj *ObjectServer) processEnvironment(overrides Value) []string {
 class:=obj.sendMessage(overrides,"class",nil)
 if !isDictionaryClass(class.Str) { panic(signalError{msg:"Process environment: requires a Dictionary"}) } else {}
 values:=map[string]string{}
 for _,entry:=range os.Environ() {
  key,value,found:=strings.Cut(entry,"=")
  if found { values[processEnvironmentKey(key)]=value } else {}
 }
 keys:=obj.readArrayElements(obj.sendMessage(overrides,"keys",nil))
 for _,key:=range keys {
  if key.Kind!="string" { panic(signalError{msg:"Environment variable names must be strings"}) } else {}
  value:=obj.sendMessage(overrides,"at:",[]Value{key})
  shellEnvironmentName(key.Str,value)
  if value.Kind=="nil" { delete(values,processEnvironmentKey(key.Str)) } else { values[processEnvironmentKey(key.Str)]=value.Str }
 }
 result:=make([]string,0,len(values))
 for key,value:=range values { result=append(result,key+"="+value) }
 return result
}

func processCommand(program string,arguments []string,directory string,environment []string) *exec.Cmd {
 command:=exec.Command(program,arguments...)
 if environment==nil { return command } else { command.Env=environment }
 if strings.ContainsAny(program,"/\\") { return command } else {}
 path:=""
 for _,entry:=range environment {
  key,value,found:=strings.Cut(entry,"=")
  if found && processEnvironmentKey(key)=="PATH" { path=value } else {}
 }
 for _,dir:=range filepath.SplitList(path) {
  if !filepath.IsAbs(dir) { dir=filepath.Join(directory,dir) } else {}
  candidate,err:=filepath.Abs(filepath.Join(dir,program))
  if err!=nil { continue } else {}
  executable,err:=exec.LookPath(candidate)
  if err==nil { command.Path=executable;command.Err=nil;return command } else {}
 }
 command.Err=&exec.Error{Name:program,Err:exec.ErrNotFound}
 return command
}

func processAppendOutput(buffer *bytes.Buffer,content []byte) {
 remaining:=processOutputCap-buffer.Len()
 if remaining<=0 { if remaining==0 && len(content)>0 { buffer.WriteString("\n…<truncated>\n") } else {};return } else if len(content)>remaining { buffer.Write(content[:remaining]);buffer.WriteString("\n…<truncated>\n") } else { buffer.Write(content) }
}

func processEnvironmentKey(name string) string {
 if runtime.GOOS=="windows" { return strings.ToUpper(name) } else { return name }
}
