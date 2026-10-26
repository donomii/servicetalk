package main

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
)

func literalWire(value Value) WireInput { return WireInput{Value:&value} }

func testWireDefinition(t *testing.T,base,variable string,nodes []WireNode) {
 t.Helper()
 body,err:=json.Marshal(WireDefinition{Nodes:nodes})
 if err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,variable+" definition: "+smalltalkString(string(body)))
}

func testWireRun(t *testing.T,base,variable,method string) WireRun {
 t.Helper()
 raw:=sourceTestEval(t,base,variable+" "+method)
 var run WireRun
 if err:=json.Unmarshal([]byte(raw),&run);err!=nil { t.Fatal(err) } else {}
 return run
}

func TestWiringTypedConnectionsAndRetainedResults(t *testing.T) {
 base:=interactiveTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"wireCounter := Counter new. wireCounter add: 7. wireCounter"})
 sourceTestEval(t,base,"graph := Wiring new")
 target:=literalWire(created.Result)
 nodes:=[]WireNode{
  {ID:"read",Target:target,Method:"value"},
  {ID:"double",Target:WireInput{From:"read"},Method:"*",Args:[]WireInput{literalWire(NumVal(2))}},
  {ID:"write",Target:target,Method:"count:",Args:[]WireInput{{From:"double"}}},
  {ID:"copy",Target:target,Method:"clone"},
  {ID:"readCopy",Target:WireInput{From:"copy"},Method:"value"},
 }
 testWireDefinition(t,base,"graph",nodes)
 run:=testWireRun(t,base,"graph","run")
 if run.State!="completed" || run.Results["readCopy"].Num!=14 || run.Results["copy"].ObjID==created.Result.ObjID || len(run.Order)!=5 { t.Fatalf("graph results: %+v",run) } else {}
 copy:=cloneTestInspect(t,base,run.Results["copy"].ObjID)
 if len(copy.Retention.Path)<3 { t.Fatalf("result not retained through graph: %+v",copy.Retention) } else {}
 first:=run.Results["copy"].ObjID
 again:=testWireRun(t,base,"graph","resume")
 if again.Results["copy"].ObjID!=first { t.Fatal("resuming a completed graph reran effects") } else {}
}

func TestWiringRecoveryDoesNotRepeatSuccessfulNodes(t *testing.T) {
 dir:=t.TempDir()
 source:="class WireGate [ | allowed total | initialize [ allowed := false. total := 0 ] prepare [ total := total + 1. ^ total ] accept: value [ ^ allowed ifTrue: [ value ] ifFalse: [ 'gate closed' signal ] ] enable [ allowed := true ] count [ ^ total ] ]"
 if err:=os.WriteFile(filepath.Join(dir,"WireGate.st"),[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 gate:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"gate := WireGate new. gate"})
 sourceTestEval(t,base,"graph := Wiring new")
 target:=literalWire(gate.Result)
 nodes:=[]WireNode{{ID:"prepare",Target:target,Method:"prepare"},{ID:"accept",Target:target,Method:"accept:",Args:[]WireInput{{From:"prepare"}}}}
 testWireDefinition(t,base,"graph",nodes)
 failed:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"graph run"})
 if !failed.IsError || !strings.Contains(failed.Content[0].Text,"gate closed") { t.Fatalf("graph swallowed signal: %+v",failed) } else {}
 run:=testWireRun(t,base,"graph","lastRun")
 if run.State!="failed" || run.FailedNode!="accept" || run.Results["prepare"].Num!=1 { t.Fatalf("lost partial results: %+v",run) } else {}
 sourceTestEval(t,base,"gate enable")
 run=testWireRun(t,base,"graph","resume")
 if run.State!="completed" || run.Results["accept"].Num!=1 || sourceTestEval(t,base,"gate count")!="1" { t.Fatalf("resume replayed earlier work: %+v",run) } else {}
 nodes[0].Label="changed"
 testWireDefinition(t,base,"graph",nodes)
 stale:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"graph resume"})
 if !stale.IsError || !strings.Contains(stale.Content[0].Text,"graph changed") { t.Fatalf("resumed changed plan: %+v",stale) } else {}
}

func TestWiringCallsForeignReceiver(t *testing.T) {
 if _,err:=exec.LookPath("cc");err!=nil { t.Skip("C compiler unavailable") } else {}
 base:=interactiveTestRuntime(t)
 foreign:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"foreignWireCounter := CCounter new. foreignWireCounter"})
 if foreign.Error!="" { t.Fatal(foreign.Error) } else {}
 sourceTestEval(t,base,"graph := Wiring new")
 testWireDefinition(t,base,"graph",[]WireNode{{ID:"add",Target:literalWire(foreign.Result),Method:"add:",Args:[]WireInput{literalWire(NumVal(9))}},{ID:"negate",Target:WireInput{From:"add"},Method:"negated"}})
 run:=testWireRun(t,base,"graph","run")
 if run.Results["negate"].Num!=-9 { t.Fatalf("foreign result connection: %+v",run) } else {}
}

func TestWiringRejectsInvalidConnections(t *testing.T) {
 for _,definition:=range []WireDefinition{
  {Nodes:[]WireNode{{ID:"a",Method:"value",Target:WireInput{From:"a"}}}},
  {Nodes:[]WireNode{{ID:"a",Method:"value",Target:WireInput{From:"missing"}}}},
  {Nodes:[]WireNode{{ID:"a",Method:"value",Target:WireInput{}}}},
  {Nodes:[]WireNode{{ID:"a",Method:"value",Target:literalWire(Nil)},{ID:"a",Method:"value",Target:literalWire(Nil)}}},
 } {
  body,_:=json.Marshal(definition)
  if _,_,err:=parseWiring(string(body));err==nil { t.Fatalf("accepted invalid graph: %s",body) } else {}
 }
}
