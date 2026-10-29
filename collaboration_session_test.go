package main

import (
 "bytes"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync"
 "testing"
 "time"
)

func sessionTestAction(t *testing.T,base string,request SessionRequest) SessionResponse {
 t.Helper();body,err:=json.Marshal(request)
 if err!=nil { t.Fatal(err) } else {}
 var response SessionResponse
 if err=json.Unmarshal(interactionTestPost(t,base,"/session",body),&response);err!=nil { t.Fatal(err) } else { return response }
 return response
}

func sessionTestJoin(t *testing.T,base,name,kind string) SessionResponse {
 t.Helper();response:=sessionTestAction(t,base,SessionRequest{Action:"join",Name:name,Kind:kind})
 if response.Error!="" || response.ParticipantID=="" || response.WorkspaceID=="" { t.Fatalf("join: %+v",response) } else { return response }
 return response
}

func sessionTestEval(t *testing.T,base,participant,code string) InteractionResult {
 t.Helper();result:=testInteraction(t,base,InteractionRequest{Kind:"eval",ParticipantID:participant,Code:code})
 if result.Error!="" || result.RecordingError!="" { t.Fatalf("%s: %+v",code,result) } else { return result }
 return result
}

func TestCollaborationSharesObjectsWithIsolatedWorkspaces(t *testing.T) {
 base:=interactiveTestRuntime(t);alice:=sessionTestJoin(t,base,"Alice","human");bob:=sessionTestJoin(t,base,"Builder","agent")
 if alice.WorkspaceID==bob.WorkspaceID || alice.SessionID!=bob.SessionID { t.Fatalf("participants do not share one session with separate workspaces: %+v %+v",alice,bob) } else {}
 created:=sessionTestEval(t,base,alice.ParticipantID,"scratch := 11. counter := Counter new. counter")
 if other:=sessionTestEval(t,base,bob.ParticipantID,"scratch");other.Result.Kind!="nil" { t.Fatalf("workspace leak: %+v",other) } else {}
 sessionTestEval(t,base,bob.ParticipantID,"scratch := 22")
 if own:=sessionTestEval(t,base,alice.ParticipantID,"scratch");own.Result.Num!=11 { t.Fatalf("scratch overwritten: %+v",own) } else {}
 zero:=int64(0)
 published:=sessionTestAction(t,base,SessionRequest{Action:"publish",ParticipantID:alice.ParticipantID,Name:"counter",Value:&created.Result,ExpectedRevision:&zero})
 if published.Error!="" || len(published.Publications)!=1 || published.Publications[0].Revision!=1 { t.Fatalf("publish: %+v",published) } else {}
 bound:=sessionTestAction(t,base,SessionRequest{Action:"bind",ParticipantID:bob.ParticipantID,Name:"counter",Binding:"shared"})
 if bound.Error!="" { t.Fatal(bound.Error) } else {}
 if read:=sessionTestEval(t,base,bob.ParticipantID,"shared");read.Result.ObjID!=created.Result.ObjID { t.Fatalf("object identity changed: %+v",read) } else {}
 var group sync.WaitGroup;failures:=make(chan string,2)
 for _,participant:=range []string{alice.ParticipantID,bob.ParticipantID} {
  group.Add(1);go func(id string){defer group.Done();for i:=0;i<8;i++ {
   body,_:=json.Marshal(InteractionRequest{Kind:"call",ParticipantID:id,ObjectID:created.Result.ObjID,Method:"increment"})
   response,err:=http.Post(base+"/interaction","application/json",bytes.NewReader(body))
   if err!=nil { failures<-err.Error();return } else {}
   var result InteractionResult;err=json.NewDecoder(response.Body).Decode(&result);response.Body.Close()
   if err!=nil || result.Error!="" { failures<-fmt.Sprintf("concurrent call: %+v %v",result,err);return } else {}
  }}(participant)
 }
 group.Wait();close(failures);for failure:=range failures { t.Error(failure) }
 if read:=sessionTestEval(t,base,bob.ParticipantID,"shared value");read.Result.Num!=16 { t.Fatalf("shared mutation lost: %+v",read) } else {}
 for _,request:=range []InteractionRequest{{Kind:"workspace",ParticipantID:alice.ParticipantID,ObjectID:created.Result.ObjID,Code:"note := 42"},{Kind:"workspace",ParticipantID:bob.ParticipantID,ObjectID:created.Result.ObjID,Code:"note"}} {
  result:=testInteraction(t,base,request)
  if result.Error!="" || (request.ParticipantID==bob.ParticipantID && result.Result.Kind!="nil") { t.Fatalf("object workspace isolation: %+v",result) } else {}
 }
 status:=sessionTestAction(t,base,SessionRequest{Action:"status",ParticipantID:bob.ParticipantID})
 if len(status.Activity)<20 || status.WorkspaceID!=bob.WorkspaceID { t.Fatalf("activity/status: %+v",status) } else {}
 for _,event:=range status.Activity { if event.ParticipantID=="" || event.Name=="" || event.Sequence<1 { t.Fatalf("unattributed event: %+v",event) } else {} }
}

func TestCollaborationRevisionConflictsAndTombstones(t *testing.T) {
 base:=interactiveTestRuntime(t);alice:=sessionTestJoin(t,base,"Alice","human");bob:=sessionTestJoin(t,base,"Bob","agent")
 zero:=int64(0);one:=int64(1);two:=int64(2);value:=StrVal("initial")
 initial:=sessionTestAction(t,base,SessionRequest{Action:"publish",ParticipantID:alice.ParticipantID,Name:"plan",Value:&value,ExpectedRevision:&zero})
 if initial.Error!="" { t.Fatal(initial.Error) } else {}
 replies:=make(chan SessionResponse,2);var group sync.WaitGroup
 for _,id:=range []string{alice.ParticipantID,bob.ParticipantID} { group.Add(1);go func(participant string){defer group.Done();next:=StrVal(participant);body,_:=json.Marshal(SessionRequest{Action:"publish",ParticipantID:participant,Name:"plan",Value:&next,ExpectedRevision:&one});response,err:=http.Post(base+"/session","application/json",bytes.NewReader(body));if err!=nil { replies<-SessionResponse{Error:err.Error()};return } else {};defer response.Body.Close();var result SessionResponse;if err=json.NewDecoder(response.Body).Decode(&result);err!=nil { result.Error=err.Error() } else {};replies<-result}(id) }
 group.Wait();close(replies);success,conflict:=0,0
 for reply:=range replies { if reply.Error=="" { success++ } else if strings.Contains(reply.Error,"Publication conflict") { conflict++ } else { t.Fatal(reply.Error) } }
 if success!=1 || conflict!=1 { t.Fatalf("CAS allowed conflicting authors: success=%d conflict=%d",success,conflict) } else {}
 removed:=sessionTestAction(t,base,SessionRequest{Action:"unpublish",ParticipantID:bob.ParticipantID,Name:"plan",ExpectedRevision:&two})
 if removed.Error!="" || removed.Publications[0].Present || removed.Publications[0].Revision!=3 { t.Fatalf("tombstone: %+v",removed) } else {}
 stale:=sessionTestAction(t,base,SessionRequest{Action:"publish",ParticipantID:alice.ParticipantID,Name:"plan",Value:&value,ExpectedRevision:&zero})
 if !strings.Contains(stale.Error,"Publication conflict") { t.Fatalf("deleted name lost revision: %+v",stale) } else {}
}

func TestCollaborationRetainsBoardAndScopesResultHandles(t *testing.T) {
 base:=interactiveTestRuntime(t);alice:=sessionTestJoin(t,base,"Alice","human");bob:=sessionTestJoin(t,base,"Bob","agent")
 created:=sessionTestEval(t,base,alice.ParticipantID,"Counter new")
 body,_:=json.Marshal(ResultRequest{ParticipantID:alice.ParticipantID,Action:"bind",Handle:created.Handle,Name:"named"})
 var changed InteractionResult;json.Unmarshal(interactionTestPost(t,base,"/result",body),&changed)
 if changed.Error!="" { t.Fatal(changed.Error) } else {}
 if found:=sessionTestEval(t,base,bob.ParticipantID,"named");found.Result.Kind!="nil" { t.Fatalf("named result leaked: %+v",found) } else {}
 zero:=int64(0);published:=sessionTestAction(t,base,SessionRequest{Action:"publish",ParticipantID:alice.ParticipantID,Name:"retained",Value:&created.Result,ExpectedRevision:&zero})
 if published.Error!="" { t.Fatal(published.Error) } else {}
 left:=sessionTestAction(t,base,SessionRequest{Action:"leave",ParticipantID:alice.ParticipantID})
 if left.Error!="" || left.WorkspaceID!="" || left.Participants[0].Active { t.Fatalf("leave: %+v",left) } else {}
 inspection:=cloneTestInspect(t,base,created.Result.ObjID)
 if inspection.Retention==nil || len(inspection.Retention.Path)<3 { t.Fatalf("board failed to retain object: %+v",inspection.Retention) } else {}
 for _,id:=range inspection.Retention.Path { if id==alice.WorkspaceID { t.Fatal("left participant still retains workspace") } else {} }
 rejected:=testInteraction(t,base,InteractionRequest{Kind:"call",ParticipantID:alice.ParticipantID,ObjectID:created.Result.ObjID,Method:"increment"})
 if !strings.Contains(rejected.Error,"left") { t.Fatalf("left participant executed: %+v",rejected) } else {}
 legacy:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"40 + 2"})
 if legacy.IsError || legacy.Content[0].Text!="42" { t.Fatalf("legacy workspace changed: %+v",legacy) } else {}
}

func TestCollaborationMCPParticipant(t *testing.T) {
 base:=interactiveTestRuntime(t)
 joined:=mcpTestCall(t,base,"session",MCPToolArguments{Action:"join",Name:"Agent",Kind:"agent"})
 if joined.IsError { t.Fatalf("join: %+v",joined) } else {}
 var participant SessionResponse
 if err:=json.Unmarshal([]byte(joined.Content[0].Text),&participant);err!=nil { t.Fatal(err) } else {}
 evaluated:=mcpTestCall(t,base,"eval",MCPToolArguments{ParticipantID:participant.ParticipantID,Code:"answer := 6 * 7. answer"})
 if evaluated.IsError { t.Fatalf("evaluate: %+v",evaluated) } else {}
 var result InteractionResult
 if err:=json.Unmarshal([]byte(evaluated.Content[0].Text),&result);err!=nil { t.Fatal(err) } else if result.Result.Num!=42 || result.WorkspaceID!=participant.WorkspaceID || result.Handle=="" { t.Fatalf("MCP participant result: %+v",result) } else {}
 noted:=mcpTestCall(t,base,"session",MCPToolArguments{Action:"note",ParticipantID:participant.ParticipantID,Text:"I am checking the shared counter."})
 if noted.IsError { t.Fatalf("note: %+v",noted) } else {}
 var status SessionResponse
 if err:=json.Unmarshal([]byte(noted.Content[0].Text),&status);err!=nil { t.Fatal(err) } else {}
 latest:=status.Activity[len(status.Activity)-1]
 if latest.Action!="note" || latest.Detail!="I am checking the shared counter." || latest.ParticipantID!=participant.ParticipantID { t.Fatalf("note attribution: %+v",latest) } else {}
 invalid:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:participant.SessionID,Method:"publish:value:expected:by:",Args:[]Value{StrVal("invalid"),NumVal(1),NumVal(0.5),ObjVal(participant.ParticipantID)}})
 if !invalid.IsError || !strings.Contains(invalid.Content[0].Text,"integer") { t.Fatalf("direct publication accepted fractional revision: %+v",invalid) } else {}
}

func TestCollaborationBusyWorkspaceDoesNotBlockSession(t *testing.T) {
 entered:=make(chan struct{});release:=make(chan struct{});var releaseOnce sync.Once
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){close(entered);<-release;w.Write([]byte("finished"))}))
 defer server.Close();defer releaseOnce.Do(func(){close(release)})
 base:=interactiveTestRuntime(t);alice:=sessionTestJoin(t,base,"Alice","human");bob:=sessionTestJoin(t,base,"Bob","agent")
 value:=NumVal(42);zero:=int64(0)
 published:=sessionTestAction(t,base,SessionRequest{Action:"publish",ParticipantID:bob.ParticipantID,Name:"answer",Value:&value,ExpectedRevision:&zero})
 if published.Error!="" { t.Fatal(published.Error) } else {}
 evaluated:=make(chan InteractionResult,1)
 go func(){evaluated<-testInteraction(t,base,InteractionRequest{Kind:"eval",ParticipantID:alice.ParticipantID,Code:"HttpClient new get: "+smalltalkString(server.URL)})}()
 select {case <-entered:case <-time.After(3*time.Second):t.Fatal("workspace did not begin blocking request")}
 bound:=make(chan SessionResponse,1)
 go func(){bound<-sessionTestAction(t,base,SessionRequest{Action:"bind",ParticipantID:alice.ParticipantID,Name:"answer",Binding:"answer"})}()
 // Wait for the retained transfer, proving the binding has reached the busy workspace.
 deadline:=time.Now().Add(3*time.Second)
 for {
  session:=cloneTestInspect(t,base,alice.SessionID);transfers:=cloneTestInspect(t,base,session.InstanceVars["transfers"].ObjID)
  if len(transfers.InstanceVars)>0 { break } else if time.Now().After(deadline) { t.Fatal("publication binding never prepared a transfer") } else { time.Sleep(10*time.Millisecond) }
 }
 noted:=make(chan SessionResponse,1)
 go func(){noted<-sessionTestAction(t,base,SessionRequest{Action:"note",ParticipantID:bob.ParticipantID,Text:"Other work continues."})}()
 select {case response:=<-noted:if response.Error!="" { t.Fatal(response.Error) } else {};case <-time.After(2*time.Second):t.Fatal("busy participant workspace blocked session activity/status")}
 releaseOnce.Do(func(){close(release)})
 if result:=<-evaluated;result.Error!="" { t.Fatal(result.Error) } else {}
 if response:=<-bound;response.Error!="" { t.Fatal(response.Error) } else {}
 if result:=sessionTestEval(t,base,alice.ParticipantID,"answer");result.Result.Num!=42 { t.Fatalf("binding did not finish: %+v",result) } else {}
 session:=cloneTestInspect(t,base,alice.SessionID);transfers:=cloneTestInspect(t,base,session.InstanceVars["transfers"].ObjID)
 if len(transfers.InstanceVars)!=0 { t.Fatalf("completed binding leaked transfer roots: %+v",transfers.InstanceVars) } else {}
}
