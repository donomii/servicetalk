package main

import (
 "encoding/json"
 "fmt"
 "strings"
)

// Canonical bodies identify standard forwarding controls without erasing overrides.
const replayControlMethods = `class Control [
 invoke [ wiring isNil ifTrue: [ selector = '' ifTrue: [ 'This control has no action method' signal ] ifFalse: [ ^ self effectiveReceiver perform: selector withArguments: arguments ] ] ifFalse: [ ^ wiring run ] ]
 write: value [ setter = '' ifTrue: [ 'This control has no setter' signal ] ifFalse: [ ^ self effectiveReceiver perform: setter with: value ] ]
 effectiveReceiver [ receiver isNil ifTrue: [ owner isNil ifTrue: [ ^ nil ] ifFalse: [ ^ owner effectiveReceiver ] ] ifFalse: [ ^ receiver ] ]
 ]`

func replayMethodMatches(definition *NativeDefinition,selector string,expected *MethodDef) bool {
 method,_,ok:=lookupMethod(restoreDefinition(definition),selector)
 if !ok || expected==nil || strings.Join(method.Params,"\x00")!=strings.Join(expected.Params,"\x00") || len(method.Body)!=len(expected.Body) { return false } else {}
 for i,node:=range method.Body { if node.String()!=expected.Body[i].String() { return false } else {} }
 return true
}

func replayIsControl(definition *NativeDefinition) bool {
 for current:=definition;current!=nil;current=current.Superclass { if current.Name=="Control" { return true } else {} }
 return false
}

func (c *Coordinator) replaySnapshot(id string) (NativeSnapshot,error) {
 value,err:=c.callObjectID(id,"__nativeSnapshotJson",nil,inspectionTimeout)
 if err!=nil { return NativeSnapshot{},err } else {}
 var snapshot NativeSnapshot
 err=json.Unmarshal([]byte(value.Str),&snapshot)
 return snapshot,err
}

func (c *Coordinator) controlReplayRequest(request InteractionRequest) (InteractionRequest,error) {
 if request.Kind!="call" || (request.Method!="invoke" && request.Method!="write:") { return request,nil } else {}
 snapshot,err:=c.replaySnapshot(request.ObjectID)
 if err!=nil { return request,c.controlSnapshotError(request.ObjectID,err) } else if !replayIsControl(snapshot.Definition) { return request,nil } else {}
 canonical,err:=ParseSmalltalkClassSource(replayControlMethods)
 if err!=nil { return request,err } else if !replayMethodMatches(snapshot.Definition,request.Method,canonical.Methods[request.Method]) { return request,nil } else {}
 raw,err:=c.callObjectID(request.ObjectID,"__controlState",nil,inspectionTimeout)
 if err!=nil { return request,fmt.Errorf("read control %s bindings: %w",request.ObjectID,err) } else {}
 var state ControlState
 if err=json.Unmarshal([]byte(raw.Str),&state);err!=nil { return request,err } else {}
 var properties ControlProperties
 if err=json.Unmarshal(state.Properties,&properties);err!=nil { return request,err } else {}
 replay:=request
 if request.Method=="invoke" && properties.Wiring!="" { replay.ObjectID=properties.Wiring;replay.Method="run";replay.Args=nil;return replay,nil } else {}
 receiver,standard,err:=c.replayControlReceiver(request.ObjectID,snapshot,canonical.Methods["effectiveReceiver"])
 if err!=nil { return request,err } else if !standard { return request,nil } else {}
 replay.ObjectID=receiver.ObjID
 if request.Method=="invoke" { replay.Method=properties.Method;replay.Args=append([]Value{},properties.Args...) } else { replay.Method=properties.Setter;replay.Args=append([]Value{},request.Args...) }
 if replay.Method=="" { return request,fmt.Errorf("control %s has no bound selector",request.ObjectID) } else { return replay,nil }
}

func (c *Coordinator) controlSnapshotError(id string,captureErr error) error {
 class,err:=c.callObjectID(id,"class",nil,inspectionTimeout)
 if err!=nil { return fmt.Errorf("read action receiver %s: %w",id,err) } else {}
 definition,err:=LoadClass(class.Str)
 if err==nil && replayIsControl(snapshotDefinition(definition)) { return fmt.Errorf("snapshot control %s: %w",id,captureErr) } else { return nil }
}

func (c *Coordinator) replayControlReceiver(id string,snapshot NativeSnapshot,expected *MethodDef) (Value,bool,error) {
 seen:=map[string]bool{}
 for depth:=0;depth<=controlDepth;depth++ {
  if seen[id] { return Nil,false,fmt.Errorf("control owner cycle at %s",id) } else { seen[id]=true }
  if replayIsControl(snapshot.Definition) {
   if !replayMethodMatches(snapshot.Definition,"effectiveReceiver",expected) { return Nil,false,nil } else {}
   receiver:=snapshot.Fields["receiver"]
   if receiver.Kind=="objid" { return receiver,true,nil } else if receiver.Kind!="nil" { return Nil,false,fmt.Errorf("control %s has an invalid receiver",id) } else {}
   owner:=snapshot.Fields["owner"]
   if owner.Kind!="objid" { return Nil,false,fmt.Errorf("control %s has no effective receiver",id) } else { id=owner.ObjID }
  } else {
   view,err:=ParseSmalltalkClassSource("class InspectorView [ effectiveReceiver [ ^ target ] ]")
   if err!=nil { return Nil,false,err } else if !replayMethodMatches(snapshot.Definition,"effectiveReceiver",view.Methods["effectiveReceiver"]) { return Nil,false,nil } else {}
   target:=snapshot.Fields["target"]
   if target.Kind!="objid" { return Nil,false,fmt.Errorf("view %s has no target",id) } else { return target,true,nil }
  }
  next,err:=c.replaySnapshot(id)
  if err!=nil { return Nil,false,fmt.Errorf("read control owner %s: %w",id,err) } else { snapshot=next }
 }
 return Nil,false,fmt.Errorf("control owner chain exceeds %d levels",controlDepth)
}
