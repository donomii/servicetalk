package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "strings"
)

func parseWorkbench(source string) (WorkbenchDefinition,error) {
 var definition WorkbenchDefinition
 if err:=json.Unmarshal([]byte(source),&definition);err!=nil { return definition,err } else {}
 if strings.TrimSpace(definition.Title)=="" || len(definition.Tools)>40 { return definition,fmt.Errorf("workbench needs a title and at most 40 tools") } else {}
 ids:=map[string]bool{}
 singleton:=map[string]bool{}
 for _,tool:=range definition.Tools {
  if !validBindingName(tool.ID) || ids[tool.ID] || strings.TrimSpace(tool.Label)=="" { return definition,fmt.Errorf("tool needs a unique identifier and label: %q",tool.ID) } else { ids[tool.ID]=true }
  switch tool.Action {
  case "results","record":
   if singleton[tool.Action] { return definition,fmt.Errorf("duplicate %s tool",tool.Action) } else { singleton[tool.Action]=true }
  case "recording","methods","objects","trace","debugger","wiring","examples","services","session","shell":
  case "inspect","call":
   if err:=validateDebugValues([]Value{ObjVal(tool.ObjectID)});err!=nil { return definition,err } else if strings.TrimSpace(tool.ObjectID)=="" { return definition,fmt.Errorf("tool %s needs a target object",tool.ID) } else {}
   if tool.Action=="call" && tool.Method=="" { return definition,fmt.Errorf("tool %s needs a method",tool.ID) } else if err:=validateDebugValues(tool.Args);err!=nil { return definition,err } else {}
  default:return definition,fmt.Errorf("unknown workbench action %q",tool.Action)
  }
 }
 return definition,nil
}

func (obj *ObjectServer) workbenchReferences(args []Value) Value {
 if len(args)!=1 || args[0].Kind!="string" { panic(signalError{msg:"workbenchReferences: expects a JSON definition"}) } else {}
 definition,err:=parseWorkbench(args[0].Str)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 references:=[]Value{}
 for _,tool:=range definition.Tools {
  if tool.ObjectID!="" { references=append(references,ObjVal(tool.ObjectID)) } else {}
  for _,value:=range tool.Args { if value.Kind=="objid" { references=append(references,value) } else {} }
 }
 result:=obj.newArrayFrom(references)
 if result.Kind!="objid" { panic(signalError{msg:"could not retain workbench tool references"}) } else { return result }
}

func (c *Coordinator) workbench() (WorkbenchResponse,error) {
 object,err:=c.ensureUIObject("workbench","Workbench")
 if err!=nil { return WorkbenchResponse{},err } else {}
 value,err:=c.callObjectID(object.ObjID,"definition",nil,inspectionTimeout)
 if err!=nil { return WorkbenchResponse{},err } else {}
 definition,err:=parseWorkbench(value.Str)
 return WorkbenchResponse{ObjectID:object.ObjID,Definition:definition},err
}

func (c *Coordinator) handleWorkbench(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodGet { http.Error(w,"workbench requires GET",http.StatusMethodNotAllowed);return } else {}
 if response,err:=c.workbench();err!=nil { json.NewEncoder(w).Encode(WorkbenchResponse{Error:err.Error()}) } else { json.NewEncoder(w).Encode(response) }
}

func (c *Coordinator) readExample(id string) (ExampleDescription,error) {
 result:=ExampleDescription{ObjectID:id}
 fields:=[]struct{name string;target *string}{{"title",&result.Title},{"description",&result.Description},{"source",&result.Source},{"check",&result.Check}}
 for _,field:=range fields {
  value,err:=c.callObjectID(id,field.name,nil,inspectionTimeout)
  if err!=nil { return result,err } else if value.Kind!="string" { return result,fmt.Errorf("example %s %s did not return text",id,field.name) } else { *field.target=value.Str }
 }
 workspace,err:=c.callObjectID(id,"workspace",nil,inspectionTimeout)
 if err!=nil { return result,err } else { result.Workspace=workspace.ObjID }
 return result,nil
}

func (c *Coordinator) listExamples() (ExamplesResponse,error) {
 workbench,err:=c.workbench()
 response:=ExamplesResponse{Workbench:workbench.ObjectID,Examples:[]ExampleDescription{}}
 if err!=nil { return response,err } else {}
 list,err:=c.callObjectID(workbench.ObjectID,"examples",nil,evalTimeout())
 if err!=nil { return response,err } else {}
 size,err:=c.callObjectID(list.ObjID,"size",nil,inspectionTimeout)
 if err!=nil { return response,err } else if size.Num>200 { return response,fmt.Errorf("example catalog exceeds 200 entries") } else {}
 for index:=0;index<int(size.Num);index++ {
  value,err:=c.callObjectID(list.ObjID,"at:",[]Value{NumVal(float64(index))},inspectionTimeout)
  if err!=nil { return response,err } else {}
  example,err:=c.readExample(value.ObjID)
  if err!=nil { return response,err } else { response.Examples=append(response.Examples,example) }
 }
 return response,nil
}

func (c *Coordinator) handleExamples(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodGet { http.Error(w,"examples requires GET",http.StatusMethodNotAllowed);return } else {}
 if response,err:=c.listExamples();err!=nil { json.NewEncoder(w).Encode(ExamplesResponse{Error:err.Error()}) } else { json.NewEncoder(w).Encode(response) }
}
