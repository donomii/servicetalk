package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "testing"
)

func readTraceTest(t *testing.T,base string) TraceSnapshot {
 t.Helper()
 response,err:=http.Get(base+"/trace")
 if err!=nil { t.Fatal(err) } else {}
 defer response.Body.Close()
 var trace TraceSnapshot
 if err:=json.NewDecoder(response.Body).Decode(&trace);err!=nil { t.Fatal(err) } else {}
 return trace
}

func TestTraceAcrossProcessesAndNewObjects(t *testing.T) {
 base:=interactiveTestRuntime(t)
 interactionTestPost(t,base,"/trace",[]byte("{\"action\":\"start\"}"))
 result:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"tracedCounter := Counter new. tracedCounter add: 7. tracedCounter value"})
 if result.Error!="" || result.Result.Num!=7 { t.Fatalf("eval: %+v",result) } else {}
 testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"'trace failure' signal"})
 trace:=readTraceTest(t,base)
 if !trace.Enabled || len(trace.Errors)>0 { t.Fatalf("trace: %+v",trace) } else {}
 var add,read,eval TraceEvent
 var signaled bool
 for _,event:=range trace.Events {
  switch event.Method {
  case "add:":add=event
  case "value":read=event
  case "evalResult:":if event.Result.Kind=="string" && event.Error=="" { eval=event } else if event.Error=="trace failure" { signaled=true } else {}
  }
 }
 if add.Result.Num!=7 || read.Result.Num!=7 || add.Sender=="" || add.Receiver==add.Sender || add.ChainID!=read.ChainID || eval.ChainID!=add.ChainID || add.State!="returned" || !signaled { t.Fatalf("trace correlation failed: add=%+v read=%+v eval=%+v signal=%v",add,read,eval,signaled) } else {}
 interactionTestPost(t,base,"/trace",[]byte("{\"action\":\"stop\"}"))
 count:=len(readTraceTest(t,base).Events)
 testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"tracedCounter add: 1"})
 if stopped:=readTraceTest(t,base);stopped.Enabled || len(stopped.Events)!=count { t.Fatalf("trace grew after stop: %d -> %d",count,len(stopped.Events)) } else {}
 interactionTestPost(t,base,"/trace",[]byte("{\"action\":\"clear\"}"))
 if cleared:=readTraceTest(t,base);len(cleared.Events)!=0 { t.Fatalf("clear retained %d events",len(cleared.Events)) } else {}
}

func TestTraceBufferBoundAndClearDuringCall(t *testing.T) {
 buffer:=&traceBuffer{}
 buffer.configure("start")
 for i:=0;i<traceCapacity+3;i++ {
  request:=MessageRequest{MsgID:fmt.Sprint(i),Method:"work"}
  generation:=buffer.begin("receiver",request)
  buffer.finish(generation,ReplyRequest{MsgID:request.MsgID,Result:NumVal(float64(i))})
 }
 snapshot:=buffer.snapshot()
 if len(snapshot.Events)!=traceCapacity || snapshot.Dropped!=3 || snapshot.Events[0].ID!="3" { t.Fatalf("unbounded trace: count=%d dropped=%d",len(snapshot.Events),snapshot.Dropped) } else {}
 generation:=buffer.begin("receiver",MessageRequest{MsgID:"inflight",Method:"work"})
 buffer.configure("clear")
 buffer.finish(generation,ReplyRequest{MsgID:"inflight",Result:True})
 if len(buffer.snapshot().Events)!=0 { t.Fatal("old completion resurrected a cleared event") } else {}
}
