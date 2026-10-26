package main

import "encoding/json"

func (obj *ObjectServer) allocateControlPlan(node *ControlPlan,existing map[string]*ControlPlan) {
 id:=node.Properties.ControlID
 if id!="" {
  previous,ok:=existing[id]
  if !ok { panic(signalError{msg:"control "+id+" does not belong to this view"}) } else if controlClass(previous.Properties.Kind)==controlClass(node.Properties.Kind) { node.Object=ObjVal(id) } else {}
 } else {}
 for _,child:=range node.Children { obj.allocateControlPlan(child,existing) }
}

func (obj *ObjectServer) spawnControlPlan(node *ControlPlan) {
 if node.Object.Kind!="objid" {
  value,ok:=(&EvalCtx{obj:obj}).spawnInstance(controlClass(node.Properties.Kind))
  if !ok { panic(signalError{msg:"could not create "+controlClass(node.Properties.Kind)+" control"}) } else { node.Object=value }
 } else {}
 for _,child:=range node.Children { obj.spawnControlPlan(child) }
}

func (obj *ObjectServer) applyControlPlan(node *ControlPlan,owner Value,used map[string]bool) {
 children:=[]Value{}
 for _,child:=range node.Children { obj.applyControlPlan(child,node.Object,used);children=append(children,child.Object) }
 list:=obj.newArrayFrom(children)
 if list.Kind!="objid" { panic(signalError{msg:"could not create control child list"}) } else {}
 delete(node.Raw,"children");delete(node.Raw,"controlId")
 node.Raw["kind"],_=json.Marshal(node.Properties.Kind)
 source,err:=json.Marshal(node.Raw)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 obj.sendMessage(node.Object,"__controlConfigure:owner:children:",[]Value{StrVal(string(source)),owner,list})
 used[node.Object.ObjID]=true
}

func (obj *ObjectServer) replaceViewControls(source string) Value {
 _,plan,err:=parseViewControls(source)
 if err!=nil { panic(signalError{msg:"invalid view layout: "+err.Error()}) } else {}
 existing:=map[string]*ControlPlan{}
 root:=obj.controlFields()["root"]
 if root.Kind=="objid" {
  raw:=obj.describeControlTree(root.ObjID,0,map[string]bool{})
  count:=0;previous,err:=parseControlNode(raw,0,map[string]bool{},&count)
  if err!=nil { panic(signalError{msg:err.Error()}) } else { collectControlPlan(previous,existing) }
 } else {}
 obj.allocateControlPlan(plan,existing)
 obj.spawnControlPlan(plan)
 used:=map[string]bool{}
 obj.applyControlPlan(plan,ObjVal(obj.id),used)
 obj.setControlFields(map[string]Value{"root":plan.Object,"definition":StrVal(source)})
 obj.detachUnusedControls(existing,used)
 return ObjVal(obj.id)
}

func (obj *ObjectServer) detachUnusedControls(existing map[string]*ControlPlan,used map[string]bool) {
 parents:=map[string]string{}
 for id,node:=range existing { for _,child:=range node.Children { parents[child.Properties.ControlID]=id } }
 for id,node:=range existing {
  if used[id] { continue } else {}
  owner:=Nil
  if parent:=parents[id];parent!="" && !used[parent] { owner=ObjVal(parent) } else {}
  children:=[]Value{}
  for _,child:=range node.Children { if !used[child.Properties.ControlID] { children=append(children,ObjVal(child.Properties.ControlID)) } else {} }
  list:=obj.newArrayFrom(children)
  if list.Kind!="objid" { panic(signalError{msg:"could not preserve detached control children"}) } else {}
  delete(node.Raw,"children");delete(node.Raw,"controlId")
  source,err:=json.Marshal(node.Raw)
  if err!=nil { panic(signalError{msg:err.Error()}) } else {}
  obj.sendMessage(ObjVal(id),"__controlConfigure:owner:children:",[]Value{StrVal(string(source)),owner,list})
 }
}

func (obj *ObjectServer) ensureViewRoot() Value {
 fields:=obj.controlFields()
 if fields["root"].Kind!="objid" {
  source:=fields["definition"].Str
  if source=="" { source="{}" } else {}
  obj.replaceViewControls(source)
 } else {}
 return obj.controlFields()["root"]
}

func (obj *ObjectServer) describeViewControls() Value {
 root:=obj.ensureViewRoot()
 raw:=obj.describeControlTree(root.ObjID,0,map[string]bool{})
 var definition map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(obj.controlFields()["definition"].Str),&definition);err!=nil { panic(signalError{msg:err.Error()}) } else {}
 if _,wrapped:=definition["body"];wrapped { definition["body"]=raw } else {
  var body map[string]json.RawMessage
  json.Unmarshal(raw,&body)
  for _,key:=range []string{"id","title"} { if value,ok:=definition[key];ok { body[key]=value } else {} }
  definition=body
 }
 result,err:=json.Marshal(definition)
 if err!=nil { panic(signalError{msg:err.Error()}) } else { return StrVal(string(result)) }
}

func (obj *ObjectServer) attachViewRoot(root Value) Value {
 if root.Kind!="objid" { panic(signalError{msg:"root: requires a control object"}) } else {}
 obj.describeControlTree(root.ObjID,0,map[string]bool{})
 owner:=obj.sendMessage(root,"__controlOwner",nil)
 if owner.Kind=="objid" && owner.ObjID!=obj.id { panic(signalError{msg:"remove the control from its current owner before using it as a view root"}) } else {}
 old:=obj.controlFields()["root"]
 obj.sendMessage(root,"__controlOwner:",[]Value{ObjVal(obj.id)})
 obj.setControlFields(map[string]Value{"root":root})
 if old.Kind=="objid" && old.ObjID!=root.ObjID { obj.sendMessage(old,"__controlOwner:",[]Value{Nil}) } else {}
 return ObjVal(obj.id)
}

func (obj *ObjectServer) viewControlDispatch(name string,args []Value) Value {
 switch name {
 case "__describeView":return obj.describeViewControls()
 case "__viewRoot":return obj.ensureViewRoot()
 case "__viewRoot:":
  if len(args)!=1 { panic(signalError{msg:"root: requires one control"}) } else { return obj.attachViewRoot(args[0]) }
 default:
  if len(args)!=1 || args[0].Kind!="string" { panic(signalError{msg:"definition: requires a JSON layout"}) } else { return obj.replaceViewControls(args[0].Str) }
 }
}
