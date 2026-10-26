package main

import (
 "encoding/json"
 "fmt"
 "net/http"
)

const wiringNodeLimit = 128

func parseWiring(source string) (WireDefinition,[]WireNode,error) {
 var definition WireDefinition
 if err:=json.Unmarshal([]byte(source),&definition);err!=nil { return definition,nil,err } else if len(definition.Nodes)>wiringNodeLimit { return definition,nil,fmt.Errorf("message graph exceeds %d nodes",wiringNodeLimit) } else {}
 nodes:=map[string]WireNode{}
 for _,node:=range definition.Nodes {
  if node.ID=="" || node.Method=="" { return definition,nil,fmt.Errorf("every graph node needs an ID and method") } else if _,exists:=nodes[node.ID];exists { return definition,nil,fmt.Errorf("duplicate graph node %q",node.ID) } else { nodes[node.ID]=node }
 }
 for _,node:=range definition.Nodes {
  for _,input:=range append([]WireInput{node.Target},node.Args...) {
   if (input.From=="")==(input.Value==nil) { return definition,nil,fmt.Errorf("node %s input needs exactly one literal value or connection",node.ID) } else if input.Value!=nil {
    if err:=validateDebugValues([]Value{*input.Value});err!=nil { return definition,nil,err } else {}
   } else if _,exists:=nodes[input.From];!exists { return definition,nil,fmt.Errorf("node %s connects to missing node %s",node.ID,input.From) } else {}
  }
 }
 order:=[]WireNode{}
 visited:=map[string]bool{}
 for len(order)<len(nodes) {
  previous:=len(order)
  for _,node:=range definition.Nodes {
   if visited[node.ID] || !wireDependenciesReady(node,visited) { continue } else { order=append(order,node);visited[node.ID]=true }
  }
  if len(order)==previous { return definition,nil,fmt.Errorf("message graph contains a connection cycle") } else {}
 }
 return definition,order,nil
}

func wireDependenciesReady(node WireNode,ready map[string]bool) bool {
 for _,input:=range append([]WireInput{node.Target},node.Args...) { if input.From!="" && !ready[input.From] { return false } else {} }
 return true
}

func resolveWireInput(input WireInput,results map[string]Value) Value {
 if input.Value!=nil { return *input.Value } else { return results[input.From] }
}

func (obj *ObjectServer) retainWiring(definition WireDefinition,retained Value) {
 for _,node:=range definition.Nodes {
  for _,input:=range append([]WireInput{node.Target},node.Args...) {
   if input.Value!=nil && input.Value.Kind=="objid" { obj.sendMessage(retained,"add:",[]Value{*input.Value}) } else {}
  }
 }
}

func (obj *ObjectServer) reportWiring(run WireRun) string {
 body,err:=json.Marshal(run)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 report:=string(body)
 if classAllowsSetVar(obj.classDef,"lastRun") { obj.callMethod("setVar:to:",[]Value{StrVal("lastRun"),StrVal(report)}) } else {}
 return report
}

func (obj *ObjectServer) executeWiring(source string,retained Value,previous string) Value {
 _,order,err:=parseWiring(source)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 run:=WireRun{DefinitionVersion:sourceDigest(source),Results:map[string]Value{},Order:[]string{},State:"running"}
 if previous!="" {
  if err=json.Unmarshal([]byte(previous),&run);err!=nil { panic(signalError{msg:err.Error()}) } else if run.DefinitionVersion!=sourceDigest(source) { panic(signalError{msg:"graph changed; run the new graph before resuming it"}) } else if run.State!="failed" && run.State!="completed" { panic(signalError{msg:"graph has no resumable run"}) } else {}
 } else {}
 if run.Results==nil { run.Results=map[string]Value{} } else {}
 run.State="running";run.Error="";run.FailedNode=""
 for _,node:=range order {
  if _,done:=run.Results[node.ID];done { continue } else {}
  reply:=dispatchToReply(node.ID,func() Value { return obj.executeWireNode(node,run.Results) })
  if reply.Signaled {
   run.State="failed";run.Error=reply.SignalMsg;run.FailedNode=node.ID
   obj.reportWiring(run)
   panic(signalError{msg:fmt.Sprintf("graph node %s (%s): %s",node.ID,node.Method,reply.SignalMsg)})
  } else {}
  if reply.Result.Kind=="objid" { obj.sendMessage(retained,"add:",[]Value{reply.Result}) } else {}
  run.Results[node.ID]=reply.Result;run.Order=append(run.Order,node.ID)
  obj.reportWiring(run)
 }
 run.State="completed"
 return StrVal(obj.reportWiring(run))
}

func (obj *ObjectServer) executeWireNode(node WireNode,results map[string]Value) Value {
 target:=resolveWireInput(node.Target,results)
 args:=make([]Value,len(node.Args))
 for i,input:=range node.Args { args[i]=resolveWireInput(input,results) }
 return (&EvalCtx{obj:obj}).dispatchValue(target,node.Method,args)
}

func (obj *ObjectServer) wiringDispatch(name string,args []Value) Value {
 if len(args)==0 || args[0].Kind!="string" { panic(signalError{msg:"wiring operation requires a JSON definition"}) } else {}
 definition,_,err:=parseWiring(args[0].Str)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 if name=="validateWiring:" { return args[0] } else if len(args)<2 || args[1].Kind!="objid" { panic(signalError{msg:"wiring operation requires an Array to retain references"}) } else {}
 if name=="retainWiring:in:" { obj.retainWiring(definition,args[1]);return args[1] } else {}
 previous:=""
 if name=="executeWiring:retain:previous:" {
  if len(args)!=3 || args[2].Kind!="string" { panic(signalError{msg:"resume requires the previous run report"}) } else { previous=args[2].Str }
 } else {}
 return obj.executeWiring(args[0].Str,args[1],previous)
}

func (c *Coordinator) handleWiring(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodGet { http.Error(w,"wiring requires GET",http.StatusMethodNotAllowed);return } else {}
 result:=WiringResponse{}
 id:=r.URL.Query().Get("id")
 if id=="" {
  value,err:=c.ensureUIObject("wiring:main","Wiring")
  if err!=nil { result.Error=err.Error();json.NewEncoder(w).Encode(result);return } else { id=value.ObjID }
 } else {}
 result.ObjectID=id
 definition,err:=c.callObjectID(id,"definition",nil,inspectionTimeout)
 if err==nil { err=json.Unmarshal([]byte(definition.Str),&result.Definition) } else {}
 if err==nil {
  value,readErr:=c.callObjectID(id,"lastRun",nil,inspectionTimeout)
  if readErr==nil { err=json.Unmarshal([]byte(value.Str),&result.Run) } else { err=readErr }
 } else {}
 if err!=nil { result.Error=err.Error() } else {}
 json.NewEncoder(w).Encode(result)
}
