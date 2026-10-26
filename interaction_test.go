package main

import (
 "bytes"
 "encoding/json"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func interactiveTestRuntime(t *testing.T) string {
 t.Helper()
 dir:=t.TempDir()
 for _,name:=range []string{"Counter.smt","Thermometer.st","ObjectWorkspace.smt","InteractionLog.st","InspectorView.st"} {
  source,err:=os.ReadFile(name)
  if err!=nil{t.Fatal(err)}else{}
  if err:=os.WriteFile(filepath.Join(dir,name),source,0600);err!=nil{t.Fatal(err)}else{}
 }
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 return startMCPTestCoordinator(t,broker.ClientURL(),dir)
}

func interactionTestPost(t *testing.T,base,path string,body []byte) []byte {
 t.Helper()
 response,err:=http.Post(base+path,"application/json",bytes.NewReader(body))
 if err!=nil{t.Fatal(err)}else{}
 defer response.Body.Close()
 var raw json.RawMessage
 if err:=json.NewDecoder(response.Body).Decode(&raw);err!=nil{t.Fatal(err)}else{}
 return raw
}

func testInteraction(t *testing.T,base string,request InteractionRequest) InteractionResult {
 t.Helper()
 body,err:=json.Marshal(request)
 if err!=nil{t.Fatal(err)}else{}
 var result InteractionResult
 if err:=json.Unmarshal(interactionTestPost(t,base,"/interaction",body),&result);err!=nil{t.Fatal(err)}else{}
 return result
}

func TestTypedResultsRetentionAndRecordingReplay(t *testing.T) {
 base:=interactiveTestRuntime(t)
 var recording RecordingResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/recording",[]byte("{\"action\":\"start\"}")),&recording);err!=nil{t.Fatal(err)}else{}
 if recording.Error!="" || recording.ObjectID==""{t.Fatalf("start: %+v",recording)}else{}
 result:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new",RecordingID:recording.ObjectID})
 if result.Error!="" || result.RecordingError!="" || result.Result.Kind!="objid" || result.Handle==""{t.Fatalf("object result: %+v",result)}else{}
 target:=result.Result.ObjID
 for _,request:=range []InteractionRequest{
  {Kind:"call",ObjectID:target,Method:"add:",Args:[]Value{NumVal(7)}},
  {Kind:"workspace",ObjectID:target,Code:"saved := self value. saved * 3"},
  {Kind:"eval",Code:"'expected failure' signal"},
  {Kind:"eval",Code:"$猫"},
 } {
  request.RecordingID=recording.ObjectID
  reply:=testInteraction(t,base,request)
  if reply.RecordingError!=""{t.Fatalf("record: %+v",reply)}else{}
  if request.Code=="'expected failure' signal" && !strings.Contains(reply.Error,"expected failure"){t.Fatalf("missing signal: %+v",reply)}else{}
 }
 testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"nil"})
 retained:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:result.Handle+" value"})
 if retained.IsError || retained.Content[0].Text!="7"{t.Fatalf("retained identity: %+v",retained)}else{}
 body,_:=json.Marshal(RecordingRequest{Action:"read",ObjectID:recording.ObjectID})
 if err:=json.Unmarshal(interactionTestPost(t,base,"/recording",body),&recording);err!=nil{t.Fatal(err)}else{}
 if recording.Error!="" || len(recording.Records)!=5{t.Fatalf("read: %+v",recording)}else{}
 replay:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:recording.Test})
 if replay.Error!=""{t.Fatalf("replay test: %s\n%s",replay.Error,recording.Test)}else{}
 original:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:target,Method:"value"})
 if original.IsError || !strings.Contains(original.Content[0].Text,"7"){t.Fatalf("replay changed original: %+v",original)}else{}
 for _,request:=range []ResultRequest{{Action:"bind",Handle:result.Handle,Name:"keptCounter"},{Action:"release",Handle:result.Handle}} {
  body,_:=json.Marshal(request)
  var reply InteractionResult
  if err:=json.Unmarshal(interactionTestPost(t,base,"/result",body),&reply);err!=nil{t.Fatal(err)}else{}
  if reply.Error!=""{t.Fatalf("result action: %+v",reply)}else{}
 }
 bound:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"keptCounter value"})
 if bound.IsError || bound.Content[0].Text!="7"{t.Fatalf("named result: %+v",bound)}else{}
}

func TestInspectorViewObjectPreservesEditedLayout(t *testing.T) {
 base:=interactiveTestRuntime(t)
 result:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Counter new"})
 if result.Error!=""{t.Fatal(result.Error)}else{}
 request:=InspectorViewRequest{Target:result.Result.ObjID,Key:"counter",Definition:json.RawMessage("{\"id\":\"counter\",\"body\":{\"kind\":\"value\",\"getter\":\"value\"}}")}
 body,_:=json.Marshal(request)
 var view InspectorViewResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/view",body),&view);err!=nil{t.Fatal(err)}else{}
 if view.Error!="" || view.ObjectID==""{t.Fatalf("view: %+v",view)}else{}
 changed:=`{"id":"counter","body":{"kind":"text","text":"my layout"}}`
 edit:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:view.ObjectID,Method:"definition:",Args:[]Value{StrVal(changed)}})
 if edit.Error!=""{t.Fatal(edit.Error)}else{}
 var reopened InspectorViewResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/view",body),&reopened);err!=nil{t.Fatal(err)}else{}
 if reopened.ObjectID!=view.ObjectID || !strings.Contains(string(reopened.Definition),"my layout"){t.Fatalf("edited view lost: %+v",reopened)}else{}
}

func TestObjectSuppliedTableData(t *testing.T) {
 base:=interactiveTestRuntime(t)
 object:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"Thermometer new"})
 if object.Error!=""{t.Fatal(object.Error)}else{}
 view:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:object.Result.ObjID,Method:"inspectorViews"})
 if !json.Valid([]byte(view.Result.Str)) || !strings.Contains(view.Result.Str,"readings"){t.Fatalf("views: %+v",view)}else{}
 testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:object.Result.ObjID,Method:"celsius:",Args:[]Value{NumVal(30)}})
 readings:=testInteraction(t,base,InteractionRequest{Kind:"call",ObjectID:object.Result.ObjID,Method:"readings"})
 var rows []struct{Unit string;Value float64}
 if err:=json.Unmarshal([]byte(readings.Result.Str),&rows);err!=nil{t.Fatal(err)}else{}
 if len(rows)!=2 || rows[0].Value!=30 || rows[1].Value!=86{t.Fatalf("computed table: %+v",rows)}else{}
}
