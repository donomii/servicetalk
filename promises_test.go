package main

import (
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func TestPromisePendingInspectionAndAwait(t *testing.T) {
 entered:=make(chan struct{});release:=make(chan struct{})
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){close(entered);<-release;w.Write([]byte("finished"))}))
 defer server.Close()
 released:=false
 defer func(){if !released { close(release) } else {}}()
 base:=interactiveTestRuntime(t)
 response:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"client := HttpClient new. pending := Promise new. pending send: client selector: 'get:' arguments: {"+smalltalkString(server.URL)+"} timeoutMs: 3000. pending"})
 if response.Error!="" { t.Fatal(response.Error) } else {}
 select { case <-entered: case <-time.After(2*time.Second):t.Fatal("asynchronous request did not start") }
 inspected:=cloneTestInspect(t,base,response.Result.ObjID)
 if inspected.Runtime.Promise==nil || inspected.Runtime.Promise.State!="pending" || len(inspected.Runtime.Calls)==0 { t.Fatalf("pending call not inspectable: %+v",inspected.Runtime) } else {}
 if value:=sourceTestEval(t,base,"6 * 7");value!="42" { t.Fatalf("workspace stalled: %s",value) } else {}
 for _,code:=range []string{"pending await: 10","pending clone","pending result"} {
  result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
  if !result.IsError { t.Fatalf("expected pending operation failure: %s: %+v",code,result) } else {}
 }
 close(release);released=true
 if value:=sourceTestEval(t,base,"(pending await: 2000) at: 'body'");value!="finished" { t.Fatalf("await result: %s",value) } else {}
 if value:=sourceTestEval(t,base,"pending ready");value!="true" { t.Fatalf("fulfilled promise not ready: %s",value) } else {}
 if value:=sourceTestEval(t,base,"copy := pending clone. copy result at: 'body'");value!="finished" { t.Fatalf("settled promise clone: %s",value) } else {}
}

func TestPromiseTypedObjectResultsAndArgumentSnapshot(t *testing.T) {
 base:=interactiveTestRuntime(t)
 counter:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"counter := Counter new. counter add: 7. counter"})
 response:=testInteraction(t,base,InteractionRequest{Kind:"promise",ObjectID:counter.Result.ObjID,Method:"clone"})
 if response.Error!="" || response.Result.Kind!="objid" { t.Fatalf("async interaction: %+v",response) } else {}
 copy:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:response.Result.ObjID,Method:"await:",Args:[]Value{NumVal(3000)}})
 if copy.IsError { t.Fatal(copy.Content) } else {}
 promise:=cloneTestInspect(t,base,response.Result.ObjID)
 result:=promise.Runtime.Promise.Result
 if result.Kind!="objid" || result.ObjID==counter.Result.ObjID { t.Fatalf("lost clone identity: %+v",result) } else {}
 copied:=cloneTestInspect(t,base,result.ObjID)
 if copied.InstanceVars["count"].Num!=7 || len(copied.Retention.Path)<3 { t.Fatalf("promise result retention: %+v",copied) } else {}
 if value:=sourceTestEval(t,base,"args := {counter}. indices := {0}. p := Promise new. p send: args selector: 'at:' arguments: indices. indices at: 0 put: 9. p await: 3000. (p arguments) at: 0");value!="0" { t.Fatalf("argument snapshot: %s",value) } else {}
 again:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"p send: counter selector: 'increment' arguments: #()"})
 if !again.IsError { t.Fatal("Promise reused and repeated work") } else {}
}

func TestPromiseSignalsAndTimeouts(t *testing.T) {
 dir:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(dir,"PromiseFailure.st"),[]byte("class PromiseFailure [ fail [ 'planned failure' signal ] ]"),0600);err!=nil { t.Fatal(err) } else {}
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 sourceTestEval(t,base,"failure := PromiseFailure new. rejected := Promise new. rejected send: failure selector: 'fail' arguments: #()")
 result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"rejected await: 3000"})
 if !result.IsError || !strings.Contains(result.Content[0].Text,"planned failure") { t.Fatalf("lost asynchronous signal: %+v",result) } else {}
 if state:=sourceTestEval(t,base,"rejected state");state!="rejected" { t.Fatalf("wrong state: %s",state) } else {}
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){time.Sleep(150*time.Millisecond);w.Write([]byte("late"))}));defer server.Close()
 sourceTestEval(t,base,"client := HttpClient new. timed := Promise new. timed send: client selector: 'get:' arguments: {"+smalltalkString(server.URL)+"} timeoutMs: 20")
 result=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"timed await: 3000"})
 if !result.IsError || !strings.Contains(result.Content[0].Text,"reply deadline") { t.Fatalf("timeout not visible: %+v",result) } else {}
}

func TestPromiseForeignMessage(t *testing.T) {
 if _,err:=exec.LookPath("cc");err!=nil { t.Skip("C compiler unavailable") } else {}
 base:=interactiveTestRuntime(t)
 value:=sourceTestEval(t,base,"foreign := CCounter new. promise := Promise new. promise send: foreign selector: 'add:' arguments: {9}. promise await: 3000")
 if value!="9" { t.Fatalf("foreign asynchronous value: %s",value) } else {}
}
