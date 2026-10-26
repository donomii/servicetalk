package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func debuggerTestRuntime(t *testing.T) string {
 t.Helper()
 dir:=t.TempDir()
 source:="class DebugProbe [ | total tail | initialize [ total := 0. tail := 0 ] run: n [ total := total + 1. total := total + n. tail := tail + 1. ^ total ] fail [ total := total + 1. 'broken' signal. tail := tail + 1. ^ total ] value [ ^ total ] ]"
 if err:=os.WriteFile(filepath.Join(dir,"DebugProbe.st"),[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 return startMCPTestCoordinator(t,broker.ClientURL(),dir)
}

func debuggerTestRequest(t *testing.T,base string,request DebugHTTPRequest) DebugHTTPResponse {
 t.Helper()
 body,_:=json.Marshal(request)
 var response DebugHTTPResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/debugger",body),&response);err!=nil { t.Fatal(err) } else {}
 return response
}

func waitDebugState(t *testing.T,base,id,state string,index int) DebugSnapshot {
 t.Helper()
 deadline:=time.Now().Add(5*time.Second)
 for time.Now().Before(deadline) {
  result:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"read",DebugRequest:DebugRequest{ObjectID:id}})
  if result.Error!="" { t.Fatal(result.Error) } else {}
  if result.Snapshot.State==state && (index<0 || result.Snapshot.Index==index) { return result.Snapshot } else { time.Sleep(10*time.Millisecond) }
 }
 t.Fatalf("debugger did not reach %s at statement %d",state,index)
 return DebugSnapshot{}
}

func sendDebugTestCommand(t *testing.T,base string,state DebugSnapshot,command DebugCommand) {
 t.Helper()
 command.ID=state.ID;command.Revision=state.Revision
 response:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"command",DebugRequest:DebugRequest{ObjectID:state.ObjectID},Command:command})
 if response.Error!="" { t.Fatal(response.Error) } else {}
}

func debugTestFields(t *testing.T,base,id string) map[string]Value {
 t.Helper()
 result:=mcpTestCall(t,base,"inspect",MCPToolArguments{ObjectID:id})
 var inspection InspectResponse
 if result.IsError { t.Fatal(result.Content) } else if err:=json.Unmarshal([]byte(result.Content[0].Text),&inspection);err!=nil { t.Fatal(err) } else {}
 return inspection.InstanceVars
}

func TestDebuggerPreservesFrameAndEarlierEffects(t *testing.T) {
 base:=debuggerTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"DebugProbe new"})
 id:=created.Result.ObjID
 started:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"start",DebugRequest:DebugRequest{ObjectID:id,Method:"run:",Args:[]Value{NumVal(2)}}})
 if started.Error!="" { t.Fatal(started.Error) } else {}
 state:=waitDebugState(t,base,id,"paused",0)
 if fields:=debugTestFields(t,base,id);fields["total"].Num!=0 { t.Fatalf("executed before step: %v",fields) } else {}
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"step"})
 next:=waitDebugState(t,base,id,"paused",1)
 stale:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"command",DebugRequest:DebugRequest{ObjectID:id},Command:DebugCommand{ID:state.ID,Revision:state.Revision,Action:"continue"}})
 if !strings.Contains(stale.Error,"frame changed") { t.Fatalf("stale command accepted: %+v",stale) } else {}
 sendDebugTestCommand(t,base,next,DebugCommand{Action:"step",Arguments:map[string]Value{"n":NumVal(5)},Code:"total := total + n + 10"})
 state=waitDebugState(t,base,id,"paused",2)
 if fields:=debugTestFields(t,base,id);fields["total"].Num!=16 || fields["tail"].Num!=0 { t.Fatalf("replacement repeated earlier work: %v",fields) } else {}
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"continue"})
 state=waitDebugState(t,base,id,"completed",-1)
 if state.Result.Num!=16 || state.Arguments["n"].Num!=5 { t.Fatalf("wrong resumed result or arguments: %+v",state) } else {}
 if fields:=debugTestFields(t,base,id);fields["tail"].Num!=1 { t.Fatalf("continuation skipped final statement: %v",fields) } else {}
}

func TestDebuggerRepairSignalAndExplicitReturn(t *testing.T) {
 base:=debuggerTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"DebugProbe new"})
 id:=created.Result.ObjID
 response:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"start",DebugRequest:DebugRequest{ObjectID:id,Method:"fail"}})
 if response.Error!="" { t.Fatal(response.Error) } else {}
 state:=waitDebugState(t,base,id,"paused",0)
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"continue"})
 state=waitDebugState(t,base,id,"paused",1)
 if state.Error!="broken" { t.Fatalf("missing signal: %+v",state) } else {}
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"retry",Code:"total := total + 100"})
 state=waitDebugState(t,base,id,"paused",2)
 if fields:=debugTestFields(t,base,id);fields["total"].Num!=101 { t.Fatalf("retry restarted method: %v",fields) } else {}
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"return",Value:StrVal("recovered")})
 state=waitDebugState(t,base,id,"completed",-1)
 if state.Result.Str!="recovered" || debugTestFields(t,base,id)["tail"].Num!=0 { t.Fatalf("explicit return failed: %+v",state) } else {}
}

func TestDebuggerAbortAndInvalidMethod(t *testing.T) {
 base:=debuggerTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"DebugProbe new"})
 id:=created.Result.ObjID
 missing:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"start",DebugRequest:DebugRequest{ObjectID:id,Method:"class"}})
 if !strings.Contains(missing.Error,"native method") { t.Fatalf("accepted unsupported dispatch: %+v",missing) } else {}
 debuggerTestRequest(t,base,DebugHTTPRequest{Action:"start",DebugRequest:DebugRequest{ObjectID:id,Method:"run:",Args:[]Value{NumVal(2)}}})
 state:=waitDebugState(t,base,id,"paused",0)
 duplicate:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"start",DebugRequest:DebugRequest{ObjectID:id,Method:"fail"}})
 if !strings.Contains(duplicate.Error,"already has") { t.Fatalf("duplicate session: %+v",duplicate) } else {}
 invalid:=DebugCommand{ID:state.ID,Revision:state.Revision,Action:"continue",Code:"["}
 response:=debuggerTestRequest(t,base,DebugHTTPRequest{Action:"command",DebugRequest:DebugRequest{ObjectID:id},Command:invalid})
 if response.Error=="" { t.Fatal("accepted invalid replacement") } else {}
 sendDebugTestCommand(t,base,state,DebugCommand{Action:"abort"})
 waitDebugState(t,base,id,"aborted",-1)
 value:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:id,Method:"value"})
 if value.Error!="" || value.Result.Num!=0 { t.Fatalf("abort left mailbox blocked: %+v",value) } else {}
}
