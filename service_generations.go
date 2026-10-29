package main

import (
 "bytes"
 "encoding/json"
 "fmt"
 "net/http"
 "time"
)

func (obj *ObjectServer) isService() bool {
 for class:=obj.classDef;class!=nil;class=class.Superclass { if class.Name=="Service" { return true } else {} }
 return false
}

// Standard Service methods are short wrappers over atomic state changes or independent routed calls.
func (obj *ObjectServer) serviceConcurrentMethod(method string) bool {
 if !obj.isService() { return false } else {}
 if method!="__service:arguments:" { if _,owner,found:=lookupMethod(obj.classDef,method);!found || owner.Name!="Service" { return false } else {} } else {}
 switch method {
 case "name","name:","status","prepareClass:","prepareSource:","check:selector:arguments:expect:","promote:","current","targetFor:","call:arguments:","call:arguments:timeoutMs:","retire:","discard:","forceRetire:","__service:arguments:":return true
 default:return false
 }
}

func (obj *ObjectServer) serviceDispatch(args []Value) Value {
 if !obj.isService() || len(args)!=2 || args[0].Kind!="string" { panic(signalError{msg:"Service operation requires a Service receiver, operation string and argument Array"}) } else {}
 values:=obj.serviceArray(args[1]);operation:=args[0].Str
 want:=1
 switch operation { case "status","current":want=0;case "check":want=4;case "call":want=3;case "name":want=len(values) }
 if len(values)!=want || (operation=="name" && want>1) { panic(signalError{msg:fmt.Sprintf("Service %s expects %d arguments, received %d",operation,want,len(values))}) } else {}
 switch operation {
 case "name","status","current","target":return obj.serviceRead(operation,values)
 case "prepareClass","prepareSource":return obj.servicePrepare(operation,serviceString(values[0],"source or class name"))
 case "check":return obj.serviceCheck(values)
 case "call":return obj.serviceCall(values)
 case "promote":
  id:=serviceString(values[0],"generation ID")
  if err:=obj.servicePromote(id);err!=nil { panic(signalError{msg:err.Error()}) } else { return StrVal(id) }
 case "retire","discard","force":
  id:=serviceString(values[0],"generation ID");target,err:=obj.serviceRetire(id,operation)
  if err!=nil { panic(signalError{msg:err.Error()}) } else {}
  if operation=="force" {
   _,_,err=obj.serviceRPC(target,"__forceShutdown",nil,5*time.Second)
   if err!=nil { _=obj.serviceForced(id,err.Error());panic(signalError{msg:"force retirement of "+id+": "+err.Error()}) } else { _=obj.serviceForced(id,"") }
  } else {}
  return StrVal(id)
 default:panic(signalError{msg:"unknown Service operation "+operation})
 }
}

func serviceString(value Value,label string) string {
 if value.Kind!="string" || value.Str=="" { panic(signalError{msg:"Service "+label+" must be a nonempty string"}) } else { return value.Str }
}

func (obj *ObjectServer) serviceArray(value Value) []Value {
 if value.Kind!="objid" { panic(signalError{msg:"Service arguments must be an Array"}) } else {}
 class:=obj.sendMessage(value,"class",nil)
 if !isArrayClass(class.Str) { panic(signalError{msg:"Service arguments must be an Array, received "+class.String()}) } else { return obj.readArrayElements(value) }
}

func (obj *ObjectServer) serviceRead(operation string,values []Value) Value {
 result:=Nil
 if operation=="name" && len(values)>0 { serviceString(values[0],"name") } else if operation=="target" { serviceString(values[0],"generation ID") } else {}
 err:=obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  switch operation {
  case "name":if len(values)>0 { state.Name=values[0].Str } else {};result=StrVal(state.Name)
  case "status":data,err:=json.Marshal(state);if err!=nil { return err } else { result=StrVal(string(data)) }
  case "current":if target,ok:=fields[serviceTargetKey(state.Current)];ok { result=target } else {}
  case "target":if _,err:=serviceGeneration(state,values[0].Str);err!=nil { return err } else if target,ok:=fields[serviceTargetKey(values[0].Str)];ok { result=target } else {}
  }
  return nil
 })
 if err!=nil { panic(signalError{msg:err.Error()}) } else { return result }
}

func (obj *ObjectServer) servicePrepare(operation,input string) Value {
 id,err:=obj.serviceReserve(input)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 className:=input;version:="";target:=""
 defer func(){
  if failure:=recover();failure!=nil {
   message:=fmt.Sprint(failure)
   if signal,ok:=failure.(signalError);ok { message=signal.msg } else {}
   _=obj.servicePrepared(id,className,version,target,message)
   panic(signalError{msg:"prepare "+id+": "+message})
  } else {}
 }()
 var bundle *SourceBundle
 if operation=="prepareSource" { className,bundle,err=serviceSourceBundle(input) } else { bundle,err=classBundleFromClassPath() }
 if err!=nil { panic(signalError{msg:err.Error()}) } else if bundle==nil { panic(signalError{msg:"no class sources available to prepare "+className}) } else { version=bundle.Digest }
 target,err=obj.serviceSpawn(className,bundle)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 if err=obj.servicePrepared(id,className,version,target,"");err!=nil { panic(signalError{msg:err.Error()}) } else {}
 _,uncertain,err:=obj.serviceRPC(target,"initialize",nil,30*time.Second)
 if err!=nil { _=obj.serviceInitialized(id,uncertain,err.Error());panic(signalError{msg:"initialize "+className+": "+err.Error()}) } else if err=obj.serviceInitialized(id,false,"");err!=nil { panic(signalError{msg:err.Error()}) } else { return StrVal(id) }
}

func (obj *ObjectServer) serviceSpawn(className string,bundle *SourceBundle) (string,error) {
 body,err:=json.Marshal(SpawnRequest{ClassName:className,SourceBundle:bundle})
 if err!=nil { return "",err } else {}
 client:=&http.Client{Timeout:spawnTimeout()+5*time.Second}
 response,err:=client.Post(obj.coordinatorURL+"/spawn","application/json",bytes.NewReader(body))
 if err!=nil { return "",fmt.Errorf("spawn service class %s: %w",className,err) } else {}
 defer response.Body.Close()
 var spawned SpawnResponse
 if err=json.NewDecoder(response.Body).Decode(&spawned);err!=nil { return "",err } else if spawned.Error!="" { return "",fmt.Errorf("spawn service class %s: %s",className,spawned.Error) } else if spawned.ObjectID=="" { return "",fmt.Errorf("spawn service class %s returned no object ID",className) } else {}
 obj.registry.Store(spawned.ObjectID,ObjectEntry{ID:spawned.ObjectID,Subject:spawned.Subject,ClassName:className})
 return spawned.ObjectID,nil
}

func (obj *ObjectServer) serviceRPC(target,selector string,args []Value,timeout time.Duration) (Value,bool,error) {
 entry,ok:=obj.lookupRegistry(target)
 if !ok { return Nil,false,fmt.Errorf("Service target %s is not registered for %s",target,selector) } else {}
 chain:=newUUID();finish:=obj.trackCall(newUUID(),chain,selector,target,"waiting");defer finish()
 reply,err:=sendNATSCallFrom(entry.Subject,selector,args,timeout,chain,obj.id)
 if err!=nil { return Nil,true,fmt.Errorf("%s on %s: %w; reply deadline %s elapsed or transport unavailable, call was not retried",selector,target,err,timeout) } else if reply.Signaled { return Nil,false,fmt.Errorf("%s",reply.SignalMsg) } else { return reply.Result,false,nil }
}

func (obj *ObjectServer) serviceCall(args []Value) Value {
 selector:=serviceString(args[0],"selector");values:=obj.serviceArray(args[1])
 timeout,ok:=messageTimeoutFromMilliseconds(args[2])
 if !ok { panic(signalError{msg:"Service call timeout must be a finite positive number of milliseconds"}) } else {}
 generation,err:=obj.serviceBegin("",false)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 return obj.serviceInvoke(generation,selector,values,timeout,nil)
}

func (obj *ObjectServer) serviceCheck(args []Value) Value {
 id:=serviceString(args[0],"generation ID");selector:=serviceString(args[1],"check selector");values:=obj.serviceArray(args[2])
 generation,err:=obj.serviceBegin(id,true)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 return obj.serviceInvoke(generation,selector,values,30*time.Second,&args[3])
}

func (obj *ObjectServer) serviceInvoke(generation ServiceGeneration,selector string,values []Value,timeout time.Duration,expected *Value) (result Value) {
 failure:="";uncertain:=false
 defer func(){
  if recovered:=recover();recovered!=nil { failure=fmt.Sprintf("Service %s %s interrupted: %v",generation.ID,selector,recovered);uncertain=true } else {}
  if err:=obj.serviceFinish(generation.ID,uncertain,expected!=nil,failure);err!=nil { failure=err.Error() } else {}
  if failure!="" { panic(signalError{msg:failure}) } else {}
 }()
 var err error
 result,uncertain,err=obj.serviceRPC(generation.Target,selector,values,timeout)
 if err!=nil { failure=err.Error() } else if expected!=nil && result!=*expected { failure=fmt.Sprintf("check %s %s returned %s (%s), expected %s (%s)",generation.ID,selector,result.String(),result.Kind,expected.String(),expected.Kind) } else {}
 return result
}
