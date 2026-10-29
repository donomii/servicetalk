package main

import (
 "encoding/json"
 "os"
 "testing"
 "time"
)

func TestProcessControlChild(t *testing.T) {
 if os.Getenv("SERVICETALK_CONTROL_HELPER")!="1" { return } else {}
 time.Sleep(15*time.Second)
 os.Exit(0)
}

func TestProcessStopAndStatusWhileWaiting(t *testing.T) {
 t.Setenv("SERVICETALK_CONTROL_HELPER","1")
 executable,err:=os.Executable()
 if err!=nil { t.Fatal(err) } else {}
 object:=&ObjectServer{id:"process-control",classDef:&ClassDef{Name:BuiltinClassProcess,Methods:map[string]*MethodDef{}}}
 object.processStartArgv(executable,[]string{"-test.run=^TestProcessControlChild$"},"")
 t.Cleanup(func(){object.processSignal("KILL")})
 object.ensureMailbox()
 waiting:=&signalingNATSResponder{done:make(chan struct{})}
 object.dispatchNATSCall(MessageRequest{MsgID:"wait",ChainID:"wait",Method:"wait"},waiting)
 deadline:=time.Now().Add(time.Second)
 for !object.isActiveChain("wait") && time.Now().Before(deadline) { time.Sleep(time.Millisecond) }
 if !object.isActiveChain("wait") { t.Fatal("wait did not enter the application mailbox") } else {}
 for _,request:=range []MessageRequest{
  {MsgID:"status",ChainID:"status",Method:"status"},
  {MsgID:"stop",ChainID:"stop",Method:"signal:",Args:[]Value{StrVal("TERM")}},
 } {
  response:=&signalingNATSResponder{done:make(chan struct{})}
  object.dispatchNATSCall(request,response)
  select { case <-response.done: case <-time.After(time.Second):t.Fatalf("%s queued behind wait",request.Method) }
  var reply ReplyRequest
  if err:=json.Unmarshal(response.body,&reply);err!=nil { t.Fatal(err) } else if reply.Signaled { t.Fatal(reply.SignalMsg) } else {}
  if request.Method=="status" && reply.Result.Str!="running" { t.Fatalf("status: %+v",reply) } else if request.Method=="signal:" && !reply.Result.Bool { t.Fatalf("stop: %+v",reply) } else {}
 }
 select { case <-waiting.done: case <-time.After(2*time.Second):t.Fatal("wait did not finish after Stop") }
}

func TestProcessControlsDoNotChangeApplicationDispatch(t *testing.T) {
 object:=&ObjectServer{classDef:&ClassDef{Name:"OrdinaryObject"}}
 for _,method:=range []string{"status","signal:","output"} {
  if object.processControlMethod(method) { t.Fatalf("ordinary %s was made concurrent",method) } else {}
 }
 object.classDef.Name=BuiltinClassProcess
 for _,method:=range []string{"wait","start:","input:","environment:"} {
  if object.processControlMethod(method) { t.Fatalf("process mutation %s bypasses serialization",method) } else {}
 }
}
