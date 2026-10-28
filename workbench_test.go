package main

import (
 "encoding/json"
 "net/http"
 "strings"
 "testing"
)

func testWorkbench(t *testing.T,base string) WorkbenchResponse {
 t.Helper()
 response,err:=http.Get(base+"/workbench")
 if err!=nil { t.Fatal(err) } else {}
 defer response.Body.Close()
 var result WorkbenchResponse
 if err=json.NewDecoder(response.Body).Decode(&result);err!=nil { t.Fatal(err) } else if result.Error!="" { t.Fatal(result.Error) } else { return result }
 return result
}

func testExamples(t *testing.T,base string) ExamplesResponse {
 t.Helper()
 response,err:=http.Get(base+"/examples")
 if err!=nil { t.Fatal(err) } else {}
 defer response.Body.Close()
 var result ExamplesResponse
 if err=json.NewDecoder(response.Body).Decode(&result);err!=nil { t.Fatal(err) } else if result.Error!="" { t.Fatal(result.Error) } else { return result }
 return result
}

func TestRunnableExamplesCatalogAndIsolation(t *testing.T) {
 base:=interactiveTestRuntime(t)
 sourceTestEval(t,base,"counter := Counter new. counter add: 99")
 catalog:=testExamples(t,base)
 if len(catalog.Examples)!=7 || catalog.Workbench=="" { t.Fatalf("catalog: %+v",catalog) } else {}
 workspaces:=map[string]bool{}
 for _,example:=range catalog.Examples {
  if workspaces[example.Workspace] || example.Workspace=="" { t.Fatalf("examples share workspace: %+v",example) } else { workspaces[example.Workspace]=true }
  result:=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:example.ObjectID})
  if result.Error!="" { t.Fatalf("%s: %s",example.Title,result.Error) } else {}
  if result.Result.Kind=="objid" {
   inspected:=cloneTestInspect(t,base,result.Result.ObjID)
   if len(inspected.Retention.Path)<2 { t.Fatalf("%s result not retained: %+v",example.Title,inspected) } else {}
  } else {}
 }
 if value:=sourceTestEval(t,base,"counter value");value!="99" { t.Fatalf("example changed main workspace: %s",value) } else {}
 again:=testExamples(t,base)
 if again.Examples[0].ObjectID!=catalog.Examples[0].ObjectID { t.Fatal("catalog recreated live examples") } else {}
}

func TestRunnableExampleChecksAndOutput(t *testing.T) {
 base:=interactiveTestRuntime(t)
 example:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"example := RunnableExample new. example updateTitle: 'Print' description: 'A printed value' source: 'Transcript show: ''hello''. 42' check: '(it = 42) ifFalse: [ ''wrong result'' signal ]'. example"})
 result:=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:example.Result.ObjID})
 if result.Error!="" || result.Result.Num!=42 || !strings.Contains(result.Output,"hello") { t.Fatalf("typed result/output: %+v",result) } else {}
 sourceTestEval(t,base,"example check: '''check failed'' signal'")
 result=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:example.Result.ObjID})
 if !strings.Contains(result.Error,"check failed") { t.Fatalf("ignored failing check: %+v",result) } else {}
 sourceTestEval(t,base,"example check: ''. example source: '''source failed'' signal'")
 result=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:example.Result.ObjID})
 if !strings.Contains(result.Error,"source failed") { t.Fatalf("ignored failing source: %+v",result) } else {}
 before:=cloneTestInspect(t,base,example.Result.ObjID).InstanceVars["workspace"]
 sourceTestEval(t,base,"example reset")
 after:=cloneTestInspect(t,base,example.Result.ObjID).InstanceVars["workspace"]
 if before.ObjID==after.ObjID { t.Fatal("reset retained old workspace") } else {}
}

func TestCapturedExampleSurvivesWorkspaceReset(t *testing.T) {
 base:=interactiveTestRuntime(t)
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"target := Counter new. target add: 7. example := RunnableExample new. example source: 'exampleReceiver value'. example check: '(it = 7) ifFalse: [ ''wrong result'' signal ]'. example capture: 'exampleReceiver' value: target. example capture: 'exampleObserved' value: 7. example"})
 if created.Error!="" { t.Fatal(created.Error) } else {}
 first:=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:created.Result.ObjID})
 if first.Error!="" || first.Result.Num!=7 { t.Fatalf("captured run: %+v",first) } else {}
 reset:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:created.Result.ObjID,Method:"reset"})
 if reset.Error!="" { t.Fatal(reset.Error) } else {}
 second:=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:created.Result.ObjID})
 if second.Error!="" || second.Result.Num!=7 { t.Fatalf("captured run after reset: %+v",second) } else {}
 replay:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"example := RunnableExample new. example source: 'it + 1'. example check: '(it = 8) ifFalse: [ ''wrong result'' signal ]'. example capture: 'it' value: 7. example"})
 if replay.Error!="" { t.Fatal(replay.Error) } else {}
 for run:=0;run<2;run++ {
  result:=testInteraction(t,base,InteractionRequest{Kind:"example",ObjectID:replay.Result.ObjID})
  if result.Error!="" || result.Result.Num!=8 { t.Fatalf("captured prior result on run %d: %+v",run,result) } else {}
 }
}

func TestWorkbenchEditableToolsAndRetention(t *testing.T) {
 base:=interactiveTestRuntime(t)
 initial:=testWorkbench(t,base)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"toolCounter := Counter new. toolCounter"})
 definition:=WorkbenchDefinition{Title:"My tools",Tools:[]WorkbenchTool{{ID:"increment",Label:"Increment counter",Action:"call",ObjectID:counter.Result.ObjID,Method:"add:",Args:[]Value{NumVal(3)}}}}
 body,_:=json.Marshal(definition)
 sourceTestEval(t,base,"#@"+initial.ObjectID+" definition: "+smalltalkString(string(body)))
 updated:=testWorkbench(t,base)
 if updated.Definition.Title!="My tools" || len(updated.Definition.Tools)!=1 { t.Fatalf("definition not saved: %+v",updated) } else {}
 target:=cloneTestInspect(t,base,counter.Result.ObjID)
 owner:=cloneTestInspect(t,base,initial.ObjectID)
 retained:=owner.InstanceVars["retained"]
 list:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:retained.ObjID,Method:"at:",Args:[]Value{NumVal(0)}})
 if list.IsError || !strings.Contains(list.Content[0].Text,target.ObjectID) { t.Fatalf("tool target not owned by workbench: %+v",list) } else {}
 invalid:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:initial.ObjectID,Method:"definition:",Args:[]Value{StrVal(`{"title":"Bad","tools":[{"id":"x","label":"X","action":"executeJavascript"}]}`)}})
 if !invalid.IsError { t.Fatal("accepted unknown tool action") } else {}
 if testWorkbench(t,base).Definition.Title!="My tools" { t.Fatal("invalid edit replaced working definition") } else {}
}
