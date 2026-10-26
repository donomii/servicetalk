package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "sort"
 "sync"
 "time"
)

const traceCapacity = 1000

type traceBuffer struct {
 mu sync.Mutex
 enabled bool
 generation uint64
 events []TraceEvent
 dropped uint64
}

func traceMethod(method string) bool {
 if isControlMethod(method) || method=="__shutdown" { return false } else { return true }
}

func (b *traceBuffer) begin(objectID string,req MessageRequest) uint64 {
 b.mu.Lock()
 defer b.mu.Unlock()
 if !b.enabled || !traceMethod(req.Method) { return 0 } else {}
 if len(b.events)>=traceCapacity { b.events=append(b.events[:0],b.events[1:]...);b.dropped++ } else {}
 event:=TraceEvent{ID:req.MsgID,ChainID:req.ChainID,Sender:req.Sender,Receiver:objectID,Method:req.Method,Args:append([]Value{},req.Args...),Started:time.Now(),State:"running",Result:Nil}
 b.events=append(b.events,event)
 return b.generation
}

func (b *traceBuffer) finish(generation uint64,reply ReplyRequest) {
 b.mu.Lock()
 defer b.mu.Unlock()
 if generation==0 || generation!=b.generation { return } else {}
 for i:=len(b.events)-1;i>=0;i-- {
  if b.events[i].ID!=reply.MsgID { continue } else {}
  event:=&b.events[i]
  event.Finished=time.Now()
  event.DurationMs=float64(event.Finished.Sub(event.Started))/float64(time.Millisecond)
  event.Result=reply.Result
  if reply.Signaled { event.State="signaled";event.Error=reply.SignalMsg } else { event.State="returned" }
  return
 }
}

func (b *traceBuffer) configure(action string) error {
 b.mu.Lock()
 defer b.mu.Unlock()
 switch action {
 case "start":b.enabled=true;b.generation++;b.events=nil;b.dropped=0
 case "stop":b.enabled=false
 case "clear":b.generation++;b.events=nil;b.dropped=0
 default:return fmt.Errorf("unknown trace action %q",action)
 }
 return nil
}

func (b *traceBuffer) snapshot() TraceSnapshot {
 b.mu.Lock()
 defer b.mu.Unlock()
 return TraceSnapshot{Enabled:b.enabled,Events:append([]TraceEvent{},b.events...),Dropped:b.dropped}
}

func (b *traceBuffer) dispatch(name string,args []Value) Value {
 if name=="__trace:" {
  if len(args)!=1 { panic(signalError{msg:"trace configuration requires start, stop or clear"}) } else if err:=b.configure(args[0].Str);err!=nil { panic(signalError{msg:err.Error()}) } else {}
 } else {}
 body,_:=json.Marshal(b.snapshot())
 return StrVal(string(body))
}

func (c *Coordinator) configureTrace(action string) TraceSnapshot {
 result:=TraceSnapshot{Events:[]TraceEvent{},Errors:[]string{}}
 if err:=c.trace.configure(action);err!=nil { result.Errors=append(result.Errors,err.Error());return result } else {}
 if action=="start" { c.traceEnabled.Store(true) } else if action=="stop" { c.traceEnabled.Store(false) } else {}
 result.Enabled=c.traceEnabled.Load()
 for _,entry:=range c.objectSnapshot() {
  if entry.ID==coordinatorID { continue } else {}
  if _,err:=c.callObjectID(entry.ID,"__trace:",[]Value{StrVal(action)},inspectionTimeout);err!=nil { result.Errors=append(result.Errors,entry.ID+": "+err.Error()) } else {}
 }
 return result
}

func (c *Coordinator) traceSnapshot(objectID,chainID string) TraceSnapshot {
 result:=c.trace.snapshot()
 result.Enabled=c.traceEnabled.Load()
 for _,entry:=range c.objectSnapshot() {
  if entry.ID==coordinatorID || (objectID!="" && objectID!=entry.ID) { continue } else {}
  value,err:=c.callObjectID(entry.ID,"__traceJson",nil,inspectionTimeout)
  var part TraceSnapshot
  if err==nil { err=json.Unmarshal([]byte(value.Str),&part) } else {}
  if err!=nil { result.Errors=append(result.Errors,entry.ID+": "+err.Error()) } else { result.Events=append(result.Events,part.Events...);result.Dropped+=part.Dropped }
 }
 filtered:=[]TraceEvent{}
 for _,event:=range result.Events {
  if (objectID=="" || event.Receiver==objectID) && (chainID=="" || event.ChainID==chainID) { filtered=append(filtered,event) } else {}
 }
 sort.Slice(filtered,func(i,j int)bool{return filtered[i].Started.Before(filtered[j].Started)})
 result.Events=filtered
 return result
}

func (c *Coordinator) handleTrace(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 switch r.Method {
 case http.MethodGet:json.NewEncoder(w).Encode(c.traceSnapshot(r.URL.Query().Get("objectId"),r.URL.Query().Get("chainId")))
 case http.MethodPost:
  r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
  var request TraceRequest
  if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { json.NewEncoder(w).Encode(TraceSnapshot{Errors:[]string{err.Error()}}) } else { json.NewEncoder(w).Encode(c.configureTrace(request.Action)) }
 default:http.Error(w,"trace requires GET or POST",http.StatusMethodNotAllowed)
 }
}
