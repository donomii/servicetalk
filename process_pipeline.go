package main

import (
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
)

func (obj *ObjectServer) processPipelineMethod(name string,args []Value) Value {
 state:=obj.processState()
 if name=="pipelineExitCodes" {
  state.mu.Lock();codes:=append([]int{},state.pipelineCodes...);state.mu.Unlock()
  values:=make([]Value,len(codes));for i,code:=range codes { values[i]=NumVal(float64(code)) }
  return obj.newArrayFrom(values)
 } else {}
 if len(args)!=3 || args[0].Kind!="objid" || args[1].Kind!="string" || args[2].Kind!="string" { panic(signalError{msg:"Pipeline start requires an Array of argument Arrays, directory and output-file path"}) } else {}
 class:=obj.sendMessage(args[0],"class",nil)
 if !isArrayClass(class.Str) { panic(signalError{msg:"Pipeline stages must be an Array"}) } else {}
 stages:=[][]string{}
 for _,stage:=range obj.readArrayElements(args[0]) {
  arguments:=obj.arrayToStrings(stage)
  if len(arguments)==0 || arguments[0]=="" { panic(signalError{msg:"Each pipeline stage requires a nonempty executable name"}) } else { stages=append(stages,arguments) }
 }
 if len(stages)==0 { panic(signalError{msg:"A pipeline requires at least one command"}) } else {}
 obj.processStartPipeline(stages,args[1].Str,args[2].Str)
 return ObjVal(obj.id)
}

func (obj *ObjectServer) processStartPipeline(stages [][]string,directory,outputPath string) {
 state:=obj.processState()
 labels:=make([]string,len(stages));for i,stage:=range stages { labels[i]=strings.Join(stage," ") }
 environment,input:=processBeginJob(state,strings.Join(labels," | "),directory)
 commands:=make([]*exec.Cmd,len(stages))
 for i,stage:=range stages {
  command:=processCommand(stage[0],stage[1:],directory,environment);command.Dir=directory
  command.Stderr=&processWriter{st:state,stream:state.errorOutput};configureJobGroup(command);commands[i]=command
 }
 commands[0].Stdin=strings.NewReader(input)
 commands[len(commands)-1].Stdout=&processWriter{st:state,stream:state.standardOutput}
 files,err:=connectPipeline(commands,directory,outputPath)
 if err!=nil { closePipelineFiles(files);processSetCommands(state,commands);processFinishJob(state,-1,err);return } else {}
 for index,command:=range commands {
  if err=command.Start();err!=nil {
   closePipelineFiles(files)
   for _,started:=range commands[:index] { signalJobGroup(started,os.Kill) }
   for _,started:=range commands[:index] { started.Wait() }
   processSetCommands(state,commands);processFinishJob(state,-1,fmt.Errorf("start pipeline stage %d (%s): %w",index+1,labels[index],err));return
  } else {}
 }
 closePipelineFiles(files)
 processSetCommands(state,commands)
 go waitPipeline(state,commands)
}

func connectPipeline(commands []*exec.Cmd,directory,outputPath string) ([]*os.File,error) {
 files:=[]*os.File{}
 for i:=0;i<len(commands)-1;i++ {
  reader,writer,err:=os.Pipe()
  if err!=nil { return files,fmt.Errorf("connect pipeline stages: %w",err) } else {}
  commands[i].Stdout=writer;commands[i+1].Stdin=reader
  files=append(files,reader,writer)
 }
 if outputPath!="" {
  if !filepath.IsAbs(outputPath) { outputPath=filepath.Join(directory,outputPath) } else {}
  file,err:=os.OpenFile(outputPath,os.O_WRONLY|os.O_CREATE|os.O_TRUNC,0644)
  if err!=nil { return files,fmt.Errorf("open pipeline output %q: %w",outputPath,err) } else {}
  commands[len(commands)-1].Stdout=file;files=append(files,file)
 } else {}
 return files,nil
}

func closePipelineFiles(files []*os.File) { for _,file:=range files { file.Close() } }

func waitPipeline(state *processState,commands []*exec.Cmd) {
 result:=0
 var failure error
 for index,command:=range commands {
  code,err:=processWaitCode(command)
  state.mu.Lock();state.pipelineCodes[index]=code;state.mu.Unlock()
  if code!=0 { result=code } else {}
  if err!=nil { failure=err } else {}
 }
 processFinishJob(state,result,failure)
}
