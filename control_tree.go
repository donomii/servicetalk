package main

import (
 "encoding/json"
 "fmt"
)

const controlLimit = 128
const controlDepth = 16

func controlClass(kind string) string {
 switch kind {
 case "action":return "Button"
 case "text","value","code":return "Label"
 case "input","slider","textarea":return "Input"
 case "row":return "Row"
 case "column":return "Column"
 case "table","list":return "Control"
 default:return ""
 }
}

func parseControlNode(source []byte,depth int,ids map[string]bool,count *int) (*ControlPlan,error) {
 node:=&ControlPlan{}
 if err:=json.Unmarshal(source,&node.Raw);err!=nil { return nil,err } else if node.Raw==nil { return nil,fmt.Errorf("a control must be a JSON object") } else {}
 if err:=json.Unmarshal(source,&node.Properties);err!=nil { return nil,fmt.Errorf("invalid control properties: %w",err) } else {}
 if node.Properties.Kind=="" { node.Properties.Kind="column" } else {}
 *count++
 if depth>controlDepth || *count>controlLimit { return nil,fmt.Errorf("view exceeds %d controls or %d nesting levels",controlLimit,controlDepth) } else {}
 if controlClass(node.Properties.Kind)=="" { return nil,fmt.Errorf("unknown control kind %q",node.Properties.Kind) } else {}
 id:=node.Properties.ControlID
 if id!="" { if ids[id] { return nil,fmt.Errorf("control %s appears more than once",id) } else { ids[id]=true } } else {}
 values:=append([]Value{},node.Properties.Args...)
 if len(values)>controlLimit { return nil,fmt.Errorf("control has more than %d arguments",controlLimit) } else {}
 for _,id:=range []string{node.Properties.Receiver,node.Properties.Wiring} { if id!="" { values=append(values,ObjVal(id)) } else {} }
 if err:=validateDebugValues(values);err!=nil { return nil,err } else {}
 if err:=validateControlOptions(node);err!=nil { return nil,err } else {}
 var children []json.RawMessage
 if raw,ok:=node.Raw["children"];ok { if err:=json.Unmarshal(raw,&children);err!=nil { return nil,fmt.Errorf("control children must be an array: %w",err) } else {} } else {}
 if len(children)>0 && node.Properties.Kind!="row" && node.Properties.Kind!="column" { return nil,fmt.Errorf("only rows and columns can contain children") } else {}
 for _,raw:=range children { child,err:=parseControlNode(raw,depth+1,ids,count);if err!=nil { return nil,err } else { node.Children=append(node.Children,child) } }
 return node,nil
}

func validateControlOptions(node *ControlPlan) error {
 if node.Properties.Kind=="slider" {
  limits:=map[string]float64{"min":0,"max":100,"step":1}
  for key:=range limits { if raw,ok:=node.Raw[key];ok { var number float64;if err:=json.Unmarshal(raw,&number);err!=nil { return fmt.Errorf("slider %s must be a number",key) } else { limits[key]=number } } else {} }
  if limits["step"]<=0 || limits["min"]>limits["max"] { return fmt.Errorf("slider needs a positive step and minimum <= maximum") } else {}
 } else {}
 if raw,ok:=node.Raw["columns"];ok {
  var columns []map[string]json.RawMessage
  if err:=json.Unmarshal(raw,&columns);err!=nil { return fmt.Errorf("table columns must be an array") } else {}
  for _,column:=range columns { var key string;if err:=json.Unmarshal(column["key"],&key);err!=nil { return fmt.Errorf("table column needs a string key") } else {} }
 } else {}
 return nil
}

func (obj *ObjectServer) describeControlTree(id string,depth int,seen map[string]bool) json.RawMessage {
 if depth>controlDepth || len(seen)>=controlLimit || seen[id] { panic(signalError{msg:"control tree contains a cycle, shared child, or exceeds its size/depth limit"}) } else { seen[id]=true }
 value:=obj.sendMessage(ObjVal(id),"__controlState",nil)
 var state ControlState
 if value.Kind!="string" { panic(signalError{msg:"control state must return JSON"}) } else if err:=json.Unmarshal([]byte(value.Str),&state);err!=nil { panic(signalError{msg:"invalid control state: "+err.Error()}) } else {}
 var properties map[string]json.RawMessage
 if err:=json.Unmarshal(state.Properties,&properties);err!=nil || properties==nil { panic(signalError{msg:"control state needs properties"}) } else {}
 children:=[]json.RawMessage{}
 for _,child:=range state.Children { if child.Kind!="objid" { panic(signalError{msg:"control children must contain objects"}) } else { children=append(children,obj.describeControlTree(child.ObjID,depth+1,seen)) } }
 if len(children)>0 { properties["children"],_=json.Marshal(children) } else { delete(properties,"children") }
 body,err:=json.Marshal(properties)
 if err!=nil { panic(signalError{msg:err.Error()}) } else { return body }
}

func collectControlPlan(node *ControlPlan,existing map[string]*ControlPlan) {
 if node.Properties.ControlID!="" { existing[node.Properties.ControlID]=node } else {}
 for _,child:=range node.Children { collectControlPlan(child,existing) }
}

func parseViewControls(source string) (map[string]json.RawMessage,*ControlPlan,error) {
 var definition map[string]json.RawMessage
 if err:=json.Unmarshal([]byte(source),&definition);err!=nil { return nil,nil,err } else if definition==nil { return nil,nil,fmt.Errorf("view definition must be an object") } else {}
 raw:=[]byte(source)
 if body,ok:=definition["body"];ok { raw=body } else {}
 count:=0
 node,err:=parseControlNode(raw,0,map[string]bool{},&count)
 return definition,node,err
}
