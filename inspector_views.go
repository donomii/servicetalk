package main

import (
 "encoding/json"
 "fmt"
 "net/http"
)

func (c *Coordinator) inspectorView(request InspectorViewRequest) (InspectorViewResponse,error) {
 if request.Target=="" || request.Key=="" {return InspectorViewResponse{},fmt.Errorf("view requires target and key")}else{}
 if !json.Valid(request.Definition) {return InspectorViewResponse{},fmt.Errorf("view definition must be valid JSON")}else{}
 view,err:=c.ensureUIObject("view:"+request.Target+":"+request.Key,"InspectorView")
 if err!=nil {return InspectorViewResponse{},err}else{}
 target,err:=c.callObjectID(view.ObjID,"target",nil,inspectionTimeout)
 if err!=nil {return InspectorViewResponse{},err}else if target.Kind=="nil" {
  if _,err=c.callObjectID(view.ObjID,"target:",[]Value{ObjVal(request.Target)},inspectionTimeout);err!=nil{return InspectorViewResponse{},err}else{}
  if _,err=c.callObjectID(view.ObjID,"definition:",[]Value{StrVal(string(request.Definition))},inspectionTimeout);err!=nil{return InspectorViewResponse{},err}else{}
 }else{}
 definition,err:=c.callObjectID(view.ObjID,"definition",nil,inspectionTimeout)
 if err!=nil {return InspectorViewResponse{},err}else if !json.Valid([]byte(definition.Str)){return InspectorViewResponse{},fmt.Errorf("view %s contains invalid JSON",view.ObjID)}else{}
 return InspectorViewResponse{ObjectID:view.ObjID,Definition:json.RawMessage(definition.Str)},nil
}

func (c *Coordinator) handleInspectorView(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost {http.Error(w,"view requires POST",http.StatusMethodNotAllowed);return}else{}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request InspectorViewRequest
 if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil {json.NewEncoder(w).Encode(InspectorViewResponse{Error:err.Error()})}else if response,err:=c.inspectorView(request);err!=nil {json.NewEncoder(w).Encode(InspectorViewResponse{Error:err.Error()})}else{json.NewEncoder(w).Encode(response)}
}
