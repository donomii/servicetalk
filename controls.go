package main

import "encoding/json"

func (obj *ObjectServer) controlFields() map[string]Value {
 obj.mu.RLock()
 defer obj.mu.RUnlock()
 return copyDebugValues(obj.instanceVars)
}

func (obj *ObjectServer) setControlFields(fields map[string]Value) {
 obj.mu.Lock()
 defer obj.mu.Unlock()
 for name,value:=range fields { obj.instanceVars[name]=value }
}

func (obj *ObjectServer) controlArray(value Value) []Value {
 if value.Kind=="nil" || value.Kind=="" { return nil } else if value.Kind!="objid" { panic(signalError{msg:"control arguments/children must be an Array"}) } else {}
 raw:=obj.sendMessage(value,"__nativeSnapshotJson",nil)
 var snapshot NativeSnapshot
 if err:=json.Unmarshal([]byte(raw.Str),&snapshot);err!=nil { panic(signalError{msg:"cannot read control Array: "+err.Error()}) } else {}
 if snapshot.Definition==nil || snapshot.Definition.Name!=BuiltinClassArray || len(snapshot.Array)>controlLimit { panic(signalError{msg:"control arguments/children must be an Array of at most 128 values"}) } else {}
 return snapshot.Array
}

func (obj *ObjectServer) controlState() Value {
 fields:=obj.controlFields()
 properties:=ControlProperties{Kind:fields["kind"].Str,ControlID:obj.id,Label:fields["label"].Str,Text:fields["text"].Str,Getter:fields["getter"].Str,Setter:fields["setter"].Str,Method:fields["selector"].Str,Type:fields["valueType"].Str,Receiver:fields["receiver"].ObjID,Wiring:fields["wiring"].ObjID,Refresh:fields["refresh"].Bool,Args:obj.controlArray(fields["arguments"])}
 var raw map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(fields["options"].Str),&raw);err!=nil || raw==nil { panic(signalError{msg:"control options must be a JSON object"}) } else {}
 body,_:=json.Marshal(properties)
 var known map[string]json.RawMessage
 json.Unmarshal(body,&known)
 for _,key:=range []string{"kind","controlId","label","text","getter","setter","method","type","receiver","wiring","refresh","args","children"} { delete(raw,key) }
 for key,value:=range known { raw[key]=value }
 body,err:=json.Marshal(raw)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 state:=ControlState{Properties:body,Children:obj.controlArray(fields["children"])}
 body,err=json.Marshal(state)
 if err!=nil { panic(signalError{msg:err.Error()}) } else { return StrVal(string(body)) }
}

func controlReference(id string) Value { if id=="" { return Nil } else { return ObjVal(id) } }

func (obj *ObjectServer) configureControl(args []Value) Value {
 if len(args)!=3 || args[0].Kind!="string" || (args[1].Kind!="objid" && args[1].Kind!="nil") { panic(signalError{msg:"configure control requires properties, owner and children"}) } else {}
 count:=0
 node,err:=parseControlNode([]byte(args[0].Str),0,map[string]bool{},&count)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 p:=node.Properties
 arguments:=obj.newArrayFrom(p.Args)
 if arguments.Kind!="objid" { panic(signalError{msg:"could not allocate control arguments"}) } else {}
 for _,key:=range []string{"kind","controlId","label","text","getter","setter","method","type","receiver","wiring","refresh","args","children"} { delete(node.Raw,key) }
 options,err:=json.Marshal(node.Raw)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 fields:=map[string]Value{"kind":StrVal(p.Kind),"label":StrVal(p.Label),"text":StrVal(p.Text),"getter":StrVal(p.Getter),"setter":StrVal(p.Setter),"selector":StrVal(p.Method),"valueType":StrVal(p.Type),"receiver":controlReference(p.Receiver),"wiring":controlReference(p.Wiring),"refresh":BoolVal(p.Refresh),"arguments":arguments,"children":args[2],"owner":args[1],"options":StrVal(string(options))}
 obj.setControlFields(fields)
 return ObjVal(obj.id)
}

func (obj *ObjectServer) changeControlChildren(children []Value) Value {
 fields:=obj.controlFields()
 if fields["kind"].Str!="row" && fields["kind"].Str!="column" { panic(signalError{msg:"only Row and Column controls accept children"}) } else {}
 seen:=map[string]bool{obj.id:true}
 for _,child:=range children {
  if child.Kind!="objid" { panic(signalError{msg:"children must be controls"}) } else {}
  obj.describeControlTree(child.ObjID,1,seen)
  owner:=obj.sendMessage(child,"__controlOwner",nil)
  if owner.Kind=="objid" && owner.ObjID!=obj.id { panic(signalError{msg:"remove the control from its current owner before adding it"}) } else {}
 }
 list:=obj.newArrayFrom(children)
 if list.Kind!="objid" { panic(signalError{msg:"could not allocate control children"}) } else {}
 kept:=map[string]bool{}
 for _,child:=range children { kept[child.ObjID]=true;obj.sendMessage(child,"__controlOwner:",[]Value{ObjVal(obj.id)}) }
 for _,child:=range obj.controlArray(fields["children"]) { if !kept[child.ObjID] { obj.sendMessage(child,"__controlOwner:",[]Value{Nil}) } else {} }
 obj.setControlFields(map[string]Value{"children":list})
 return ObjVal(obj.id)
}

func (obj *ObjectServer) controlDispatch(name string,args []Value) Value {
 if name=="__replaceView:" || name=="__describeView" || name=="__viewRoot" || name=="__viewRoot:" {
  if !classAllowsSetVar(obj.classDef,"root") || !classAllowsSetVar(obj.classDef,"definition") { panic(signalError{msg:"view operation requires an InspectorView"}) } else { return obj.viewControlDispatch(name,args) }
 } else {}
 if !classAllowsSetVar(obj.classDef,"kind") || !classAllowsSetVar(obj.classDef,"children") { panic(signalError{msg:"control operation requires a Control object"}) } else {}
 switch name {
 case "__controlState":return obj.controlState()
 case "__controlOwner":return obj.controlFields()["owner"]
 case "__controlOwner:":
  if len(args)!=1 || (args[0].Kind!="objid" && args[0].Kind!="nil") { panic(signalError{msg:"control owner must be an object or nil"}) } else { obj.setControlFields(map[string]Value{"owner":args[0]});return ObjVal(obj.id) }
 case "__describeControl":return StrVal(string(obj.describeControlTree(obj.id,0,map[string]bool{})))
 case "__controlConfigure:owner:children:":return obj.configureControl(args)
 }
 if len(args)!=1 { panic(signalError{msg:"control tree operation requires one argument"}) } else {}
 if name=="__controlChildren:" { return obj.changeControlChildren(obj.controlArray(args[0])) } else {}
 children:=obj.controlArray(obj.controlFields()["children"])
 if name=="__controlAdd:" { children=append(children,args[0]) } else {
  kept:=[]Value{};for _,child:=range children { if child.ObjID!=args[0].ObjID { kept=append(kept,child) } else {} };children=kept
 }
 return obj.changeControlChildren(children)
}
