package main

import (
 "encoding/json"
 "strings"
 "testing"
)

func controlRecording(t *testing.T,base,action,id string) RecordingResponse {
 t.Helper()
 body,_:=json.Marshal(RecordingRequest{Action:action,ObjectID:id})
 var result RecordingResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/recording",body),&result);err!=nil { t.Fatal(err) } else if result.Error!="" { t.Fatal(result.Error) } else { return result }
 return result
}

func TestControlRecordingReplaysAgainstCreatedReceiver(t *testing.T) {
 base:=interactiveTestRuntime(t)
 recording:=controlRecording(t,base,"start","")
 created:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new",RecordingID:recording.ObjectID})
 if created.Error!="" { t.Fatal(created.Error) } else {}
 view:=controlTestView(t,base,created.Result.ObjID,`{"body":{"kind":"row","children":[{"kind":"action","method":"increment"},{"kind":"input","getter":"value","setter":"count:"}]}}`)
 tree:=controlTestDefinition(t,base,view.ObjectID)
 action:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:tree.Children[0].Properties.ControlID,Method:"invoke",RecordingID:recording.ObjectID})
 if action.Error!="" || action.RecordingError!="" || action.CaptureError!="" || action.Request==nil || action.Request.ObjectID!=created.Result.ObjID || action.Request.Method!="increment" { t.Fatalf("normalized action: %+v",action) } else {}
 edit:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:tree.Children[1].Properties.ControlID,Method:"write:",Args:[]Value{NumVal(14)},RecordingID:recording.ObjectID})
 if edit.Error!="" || edit.Request==nil || edit.Request.Method!="count:" { t.Fatalf("normalized setter: %+v",edit) } else {}
 exported:=controlRecording(t,base,"read",recording.ObjectID)
 if len(exported.Records)!=3 || strings.Contains(exported.Script,tree.Children[0].Properties.ControlID) || !strings.Contains(exported.Script,"recordedResult1 increment") { t.Fatalf("wrong replay: %s",exported.Script) } else {}
 sourceTestEval(t,base,exported.Test)
 if got:=controlTestCall(t,base,created.Result.ObjID,"value");got.Num!=14 { t.Fatalf("replay changed original counter: %+v",got) } else {}
}

func TestControlRecordingPreservesOverridesAndReportsMetadataErrors(t *testing.T) {
 base:=interactiveTestRuntime(t)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new"}).Result
 view:=controlTestView(t,base,counter.ObjID,`{"body":{"kind":"action","method":"increment"}}`)
 button:=controlTestDefinition(t,base,view.ObjectID).Properties.ControlID
 recording:=controlRecording(t,base,"start","")
 controlTestCall(t,base,button,"options:",StrVal("[]"))
 action:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:button,Method:"invoke",RecordingID:recording.ObjectID})
 if action.Error!="" || action.RecordingError=="" || action.CaptureError=="" || controlTestCall(t,base,counter.ObjID,"value").Num!=1 { t.Fatalf("metadata failure lost action/error: %+v",action) } else {}
 if len(controlRecording(t,base,"read",recording.ObjectID).Records)!=0 { t.Fatal("exported misleading record after metadata failure") } else {}
 controlTestCall(t,base,button,"options:",StrVal("{}"))
 patch:=`{"methods":[{"name":"invoke","params":[],"source":"^ 42"}]}`
 customized:=controlTestCall(t,base,button,"cloneWith:",StrVal(patch))
 overridden:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:customized.ObjID,Method:"invoke",RecordingID:recording.ObjectID})
 if overridden.Result.Num!=42 || overridden.Request!=nil || overridden.RecordingError!="" { t.Fatalf("override was replaced: %+v",overridden) } else {}
 exported:=controlRecording(t,base,"read",recording.ObjectID)
 if exported.Records[0].Request.ObjectID!=customized.ObjID || exported.Records[0].Request.Method!="invoke" { t.Fatalf("override not preserved: %+v",exported.Records) } else {}
}

func TestControlRecordingNormalizesWiring(t *testing.T) {
 base:=interactiveTestRuntime(t)
 graph:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Wiring new"}).Result
 button:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"button := Button new. button wiring: #@"+graph.ObjID+". button"}).Result
 recording:=controlRecording(t,base,"start","")
 action:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:button.ObjID,Method:"invoke",RecordingID:recording.ObjectID})
 if action.Error!="" || action.RecordingError!="" || action.Request==nil || action.Request.ObjectID!=graph.ObjID || action.Request.Method!="run" { t.Fatalf("wiring action: %+v",action) } else {}
}

func TestControlRecordingCapturesBindingBeforeActionChangesIt(t *testing.T) {
 base:=interactiveTestRuntime(t)
 button:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"button := Button new. button receiver: button. button method: 'method:'. button arguments: #('changed'). button"}).Result
 recording:=controlRecording(t,base,"start","")
 action:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:button.ObjID,Method:"invoke",RecordingID:recording.ObjectID})
 if action.Error!="" || action.Request==nil || action.Request.Method!="method:" || len(action.Request.Args)!=1 || action.Request.Args[0].Str!="changed" { t.Fatalf("captured binding after mutation: %+v",action) } else {}
 if value:=controlTestCall(t,base,button.ObjID,"method");value.Str!="changed" { t.Fatal("real control action was not executed") } else {}
}
