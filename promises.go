package main

import (
 "fmt"
 "time"
)

func (obj *ObjectServer) isPromise() bool {
 for class:=obj.classDef;class!=nil;class=class.Superclass { if class.Name=="Promise" { return true } else {} }
 return false
}

func (obj *ObjectServer) promiseSnapshot() *PromiseSnapshot {
 if !obj.isPromise() { return nil } else {}
 obj.mu.RLock()
 defer obj.mu.RUnlock()
 return &PromiseSnapshot{State:obj.instanceVars["state"].Str,Target:obj.instanceVars["target"],Method:obj.instanceVars["selector"].Str,Arguments:obj.instanceVars["arguments"],Result:obj.instanceVars["result"],Error:obj.instanceVars["error"].Str,StartedAt:obj.instanceVars["startedAt"].Str,FinishedAt:obj.instanceVars["finishedAt"].Str,TimeoutMs:obj.instanceVars["timeoutMs"].Num}
}

func (obj *ObjectServer) promiseDispatch(name string,args []Value) Value {
 if !obj.isPromise() { panic(signalError{msg:"promise operations require a Promise object"}) } else {}
 switch name {
 case "startPromise:method:arguments:timeoutMs:": return obj.startPromise(args)
 case "promiseResult":return obj.promiseResult()
 case "awaitPromise:":return obj.awaitPromise(args)
 default:panic(signalError{msg:"unknown promise operation "+name})
 }
}

func (obj *ObjectServer) startPromise(args []Value) Value {
 if len(args)!=4 || args[0].Kind!="objid" || args[1].Kind!="string" || args[1].Str=="" || args[2].Kind!="objid" { panic(signalError{msg:"Promise send requires a receiver object, selector string, argument Array and positive timeout in milliseconds"}) } else {}
 timeout,ok:=messageTimeoutFromMilliseconds(args[3])
 if !ok { panic(signalError{msg:"Promise timeout must be a finite positive number of milliseconds"}) } else {}
 entry,found:=obj.lookupRegistry(args[0].ObjID)
 if !found { panic(signalError{msg:"Promise receiver is not registered: "+args[0].ObjID}) } else {}
 arrayClass:=obj.sendMessage(args[2],"class",nil)
 if !isArrayClass(arrayClass.Str) { panic(signalError{msg:"Promise arguments must be an Array"}) } else {}
 values:=obj.readArrayElements(args[2])
 if err:=validateDebugValues(values);err!=nil { panic(signalError{msg:err.Error()}) } else {}
 retained:=obj.newArrayFrom(values)
 if retained.Kind!="objid" { panic(signalError{msg:"Promise could not retain its argument snapshot"}) } else {}
 if err:=obj.beginPromise(args,retained,time.Now().UTC().Format(time.RFC3339Nano));err!=nil { panic(signalError{msg:err.Error()}) } else {}
 go obj.runPromise(entry,args[1].Str,values,timeout)
 return ObjVal(obj.id)
}

func (obj *ObjectServer) beginPromise(args []Value,retained Value,started string) error {
 obj.mu.Lock()
 defer obj.mu.Unlock()
 if obj.promiseDone!=nil || obj.instanceVars["state"].Str!="idle" { return fmt.Errorf("Promise has already started; create a new Promise for another send") } else {}
 obj.promiseDone=make(chan struct{})
 obj.instanceVars["state"]=StrVal("pending")
 obj.instanceVars["target"]=args[0]
 obj.instanceVars["selector"]=args[1]
 obj.instanceVars["arguments"]=retained
 obj.instanceVars["timeoutMs"]=args[3]
 obj.instanceVars["startedAt"]=StrVal(started)
 obj.instanceVars["finishedAt"]=StrVal("")
 obj.instanceVars["result"]=Nil
 obj.instanceVars["error"]=StrVal("")
 return nil
}

func (obj *ObjectServer) runPromise(entry ObjectEntry,method string,args []Value,timeout time.Duration) {
 result:=Nil
 failure:=""
 defer func(){
  if recovered:=recover();recovered!=nil { failure=fmt.Sprintf("asynchronous %s on %s failed: %v",method,entry.ID,recovered) } else {}
  obj.finishPromise(result,failure,time.Now().UTC().Format(time.RFC3339Nano))
 }()
 chain:=newUUID()
 finish:=obj.trackCall(newUUID(),chain,method,entry.ID,"waiting")
 defer finish()
 reply,err:=sendNATSCallFrom(entry.Subject,method,args,timeout,chain,obj.id)
 if err!=nil { failure=fmt.Sprintf("asynchronous %s on %s: %v (reply deadline %s; the receiver may still be running)",method,entry.ID,err,timeout) } else if reply.Signaled { failure=reply.SignalMsg } else { result=reply.Result }
}

func (obj *ObjectServer) finishPromise(result Value,failure,finished string) {
 obj.mu.Lock()
 defer obj.mu.Unlock()
 obj.instanceVars["result"]=result
 obj.instanceVars["error"]=StrVal(failure)
 obj.instanceVars["finishedAt"]=StrVal(finished)
 if failure=="" { obj.instanceVars["state"]=StrVal("fulfilled") } else { obj.instanceVars["state"]=StrVal("rejected") }
 close(obj.promiseDone)
}

func (obj *ObjectServer) promiseResult() Value {
 snapshot:=obj.promiseSnapshot()
 switch snapshot.State {
 case "fulfilled":return snapshot.Result
 case "rejected":panic(signalError{msg:snapshot.Error})
 default:panic(signalError{msg:"Promise is "+snapshot.State+"; its result is not ready"})
 }
}

func (obj *ObjectServer) promiseCompletion() <-chan struct{} {
 obj.mu.RLock()
 defer obj.mu.RUnlock()
 return obj.promiseDone
}

func (obj *ObjectServer) awaitPromise(args []Value) Value {
 if len(args)!=1 { panic(signalError{msg:"await: expects a positive timeout in milliseconds"}) } else {}
 timeout,ok:=messageTimeoutFromMilliseconds(args[0])
 if !ok { panic(signalError{msg:"await: expects a finite positive timeout in milliseconds"}) } else {}
 snapshot:=obj.promiseSnapshot()
 if snapshot.State!="pending" { return obj.promiseResult() } else {}
 done:=obj.promiseCompletion()
 if done==nil { panic(signalError{msg:"pending Promise has no active operation"}) } else {}
 timer:=time.NewTimer(timeout)
 defer timer.Stop()
 select {
 case <-done:return obj.promiseResult()
 case <-timer.C:panic(signalError{msg:"await: timed out; the Promise is still available for inspection or another await:"})
 }
}

func promiseExpression(request InteractionRequest,objects map[string]string) string {
 arguments:="{"
 for index,value:=range request.Args { if index>0 { arguments+=". " } else {};arguments+=valueSource(value,objects) }
 arguments+="}"
 timeout:=request.TimeoutMs
 if timeout==0 { timeout=600000 } else {}
 return "(Promise new) send: "+valueSource(ObjVal(request.ObjectID),objects)+" selector: "+smalltalkString(request.Method)+" arguments: "+arguments+" timeoutMs: "+NumVal(timeout).String()
}
