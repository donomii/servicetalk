package main

import (
 "encoding/json"
 "os/exec"
 "strings"
 "testing"
)

func controlTestCall(t *testing.T,base,id,method string,args ...Value) Value {
 t.Helper()
 result:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:id,Method:method,Args:args})
 if result.Error!="" { t.Fatalf("%s %s: %s",id,method,result.Error) } else { return result.Result }
 return Nil
}

func controlTestView(t *testing.T,base,target,source string) InspectorViewResponse {
 t.Helper()
 body,_:=json.Marshal(InspectorViewRequest{Target:target,Key:"test",Definition:json.RawMessage(source)})
 var view InspectorViewResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/view",body),&view);err!=nil { t.Fatal(err) } else if view.Error!="" { t.Fatal(view.Error) } else { return view }
 return view
}

func controlTestDefinition(t *testing.T,base,id string) *ControlPlan {
 t.Helper()
 source:=controlTestCall(t,base,id,"definition").Str
 _,node,err:=parseViewControls(source)
 if err!=nil { t.Fatal(err) } else { return node }
 return nil
}

func TestLiveControlsIdentityAndEditedProperties(t *testing.T) {
 base:=interactiveTestRuntime(t)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new"}).Result
 view:=controlTestView(t,base,counter.ObjID,`{"title":"Controls","body":{"kind":"row","children":[{"kind":"value","getter":"value"},{"kind":"action","text":"Increase","method":"increment"}]}}`)
 first:=controlTestDefinition(t,base,view.ObjectID)
 root,label,button:=first.Properties.ControlID,first.Children[0].Properties.ControlID,first.Children[1].Properties.ControlID
 if cloneTestInspect(t,base,root).ClassName!="Row" || cloneTestInspect(t,base,button).ClassName!="Button" { t.Fatal("layout did not create native control classes") } else {}
 controlTestCall(t,base,button,"text:",StrVal("Changed through MCP"))
 controlTestCall(t,base,button,"invoke")
 if value:=controlTestCall(t,base,label,"read");value.Num!=1 { t.Fatalf("button/read: %+v",value) } else {}
 updated:=controlTestDefinition(t,base,view.ObjectID)
 if updated.Children[1].Properties.Text!="Changed through MCP" { t.Fatal("definition ignored live control change") } else {}
 var raw map[string]json.RawMessage
 json.Unmarshal([]byte(controlTestCall(t,base,view.ObjectID,"definition").Str),&raw)
 var body map[string]json.RawMessage;json.Unmarshal(raw["body"],&body)
 var children []json.RawMessage;json.Unmarshal(body["children"],&children)
 children[0],children[1]=children[1],children[0];body["children"],_=json.Marshal(children);raw["body"],_=json.Marshal(body)
 reordered,_:=json.Marshal(raw);controlTestCall(t,base,view.ObjectID,"definition:",StrVal(string(reordered)))
 after:=controlTestDefinition(t,base,view.ObjectID)
 if after.Properties.ControlID!=root || after.Children[0].Properties.ControlID!=button || after.Children[1].Properties.ControlID!=label { t.Fatal("saving/reordering replaced control identities") } else {}
 bad:=strings.Replace(string(reordered),button,"unrelatedObject",1)
 rejected:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:view.ObjectID,Method:"definition:",Args:[]Value{StrVal(bad)}})
 if rejected.Error=="" || controlTestDefinition(t,base,view.ObjectID).Children[0].Properties.ControlID!=button { t.Fatal("invalid edit changed saved tree") } else {}
 controlTestCall(t,base,root,"remove:",ObjVal(button))
 if controlTestCall(t,base,button,"owner").Kind!="nil" || len(controlTestDefinition(t,base,view.ObjectID).Children)!=1 { t.Fatal("removed child retained its owner") } else {}
}

func TestLiveControlsProgrammaticCompositionAndOwnership(t *testing.T) {
 base:=interactiveTestRuntime(t)
 sourceTestEval(t,base,"c := Counter new. row := Row new. button := Button new. button receiver: c. button method: 'add:'. button arguments: #(7). row add: button. input := Input new. input receiver: c. input getter: 'value'. input setter: 'count:'. row add: input. view := InspectorView new. view target: c. view root: row. button invoke")
 if value:=sourceTestEval(t,base,"input read");value!="7" { t.Fatalf("programmatic button: %s",value) } else {}
 if value:=sourceTestEval(t,base,"input write: 21. c value");value!="21" { t.Fatalf("programmatic setter: %s",value) } else {}
 for _,source:=range []string{"row add: row","row add: button","other := Row new. other add: button","button add: input"} {
  response:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:source})
  if !response.IsError { t.Fatalf("accepted invalid ownership: %s",source) } else {}
 }
 if value:=sourceTestEval(t,base,"row children size");value!="2" { t.Fatalf("failed edit changed children: %s",value) } else {}
 sourceTestEval(t,base,"row remove: button. other add: button")
 if value:=sourceTestEval(t,base,"button owner = other");value!="true" { t.Fatalf("reparent failed: %s",value) } else {}
 sourceTestEval(t,base,"thermo := Thermometer new. input receiver: thermo. input setter: 'celsius:'")
 failed:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"input write: -300"})
 if !failed.IsError { t.Fatal("control swallowed receiver signal") } else {}
}

func TestLiveControlsReceiversWiringAndRetention(t *testing.T) {
 base:=interactiveTestRuntime(t)
 primary:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"primary := Counter new. primary"}).Result
 secondary:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"secondary := Counter new. secondary add: 10. secondary"}).Result
 sourceTestEval(t,base,"graph := Wiring new")
 testWireDefinition(t,base,"graph",[]WireNode{{ID:"read",Target:literalWire(secondary),Method:"value"},{ID:"write",Target:literalWire(primary),Method:"count:",Args:[]WireInput{{From:"read"}}}})
 graph:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"graph"}).Result
 definition:=`{"body":{"kind":"column","receiver":"`+secondary.ObjID+`","children":[{"kind":"value","getter":"value"},{"kind":"action","text":"Copy","wiring":"`+graph.ObjID+`"},{"kind":"input","getter":"value","setter":"count:","receiver":"`+primary.ObjID+`"}]}}`
 view:=controlTestView(t,base,primary.ObjID,definition)
 controls:=controlTestDefinition(t,base,view.ObjectID)
 if value:=controlTestCall(t,base,controls.Children[0].Properties.ControlID,"read");value.Num!=10 { t.Fatalf("inherited receiver: %+v",value) } else {}
 result:=controlTestCall(t,base,controls.Children[1].Properties.ControlID,"invoke")
 if !strings.Contains(result.Str,`"state":"completed"`) || controlTestCall(t,base,primary.ObjID,"value").Num!=10 { t.Fatalf("button did not run wiring: %+v",result) } else {}
 controlTestCall(t,base,controls.Children[2].Properties.ControlID,"write:",NumVal(41))
 if controlTestCall(t,base,primary.ObjID,"value").Num!=41 || controlTestCall(t,base,secondary.ObjID,"value").Num!=10 { t.Fatal("per-control receiver was ignored") } else {}
 inspected:=cloneTestInspect(t,base,controls.Children[1].Properties.ControlID)
 if inspected.InstanceVars["wiring"].ObjID!=graph.ObjID || inspected.InstanceVars["owner"].ObjID!=controls.Properties.ControlID || len(inspected.Retention.Path)<3 { t.Fatalf("control references not retained: %+v",inspected) } else {}
}

func TestLiveControlForeignReceiver(t *testing.T) {
 if _,err:=exec.LookPath("cc");err!=nil { t.Skip("C compiler unavailable") } else {}
 base:=interactiveTestRuntime(t)
 if value:=sourceTestEval(t,base,"foreign := CCounter new. button := Button new. button receiver: foreign. button method: 'add:'. button arguments: #(9). button invoke");value!="9" { t.Fatalf("foreign control call: %s",value) } else {}
}

func TestConcurrentLiveControlRenderingAndCalls(t *testing.T) {
 base:=interactiveTestRuntime(t)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new"}).Result
 view:=controlTestView(t,base,counter.ObjID,`{"body":{"kind":"row","children":[{"kind":"value","getter":"value"},{"kind":"action","method":"increment"}]}}`)
 tree:=controlTestDefinition(t,base,view.ObjectID)
 definition:=controlTestCall(t,base,view.ObjectID,"definition")
 results:=make(chan InteractionResult,40)
 for i:=0;i<10;i++ {
  for _,request:=range []InteractionRequest{{Kind:"call",ObjectID:view.ObjectID,Method:"definition"},{Kind:"call",ObjectID:view.ObjectID,Method:"definition:",Args:[]Value{definition}},{Kind:"call",ObjectID:tree.Children[0].Properties.ControlID,Method:"read"},{Kind:"call",ObjectID:tree.Children[1].Properties.ControlID,Method:"invoke"}} {
   go func(request InteractionRequest){results<-testInteraction(t,base,request)}(request)
  }
 }
 for i:=0;i<40;i++ { if result:=<-results;result.Error!="" { t.Fatalf("concurrent render/call failed: %s",result.Error) } else {} }
 if value:=controlTestCall(t,base,counter.ObjID,"value");value.Num!=10 { t.Fatalf("button actions lost/repeated: %+v",value) } else {}
}

func TestLiveControlDetachedSubtreePreservesOwnership(t *testing.T) {
 base:=interactiveTestRuntime(t)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new"}).Result
 view:=controlTestView(t,base,counter.ObjID,`{"body":{"kind":"column","children":[{"kind":"row","children":[{"kind":"action","method":"increment"},{"kind":"text","text":"kept"}]}]}}`)
 tree:=controlTestDefinition(t,base,view.ObjectID)
 row:=tree.Children[0];button,label:=row.Children[0],row.Children[1]
 source:=`{"body":{"kind":"column","controlId":"`+tree.Properties.ControlID+`","children":[{"kind":"action","controlId":"`+button.Properties.ControlID+`","method":"increment"}]}}`
 controlTestCall(t,base,view.ObjectID,"definition:",StrVal(source))
 if controlTestCall(t,base,row.Properties.ControlID,"owner").Kind!="nil" || controlTestCall(t,base,label.Properties.ControlID,"owner").ObjID!=row.Properties.ControlID { t.Fatal("detached subtree lost its internal ownership") } else {}
 if controlTestCall(t,base,button.Properties.ControlID,"owner").ObjID!=tree.Properties.ControlID { t.Fatal("moved control has incorrect owner") } else {}
 detached:=controlTestDefinition(t,base,row.Properties.ControlID)
 if len(detached.Children)!=1 || detached.Children[0].Properties.ControlID!=label.Properties.ControlID { t.Fatal("detached subtree retained a moved child") } else {}
}
