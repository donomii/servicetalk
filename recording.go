package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "strings"
)

func (c *Coordinator) readRecording(id string) ([]InteractionRecord,error) {
 list,err:=c.callObjectID(id,"entries",nil,inspectionTimeout)
 if err!=nil { return nil,err } else if list.Kind!="objid" { return nil,fmt.Errorf("recording %s returned no entries object",id) } else {}
 count,err:=c.callObjectID(list.ObjID,"size",nil,inspectionTimeout)
 if err!=nil { return nil,err } else {}
 records:=[]InteractionRecord{}
 for i:=0;i<int(count.Num);i++ {
  value,err:=c.callObjectID(list.ObjID,"at:",[]Value{NumVal(float64(i))},inspectionTimeout)
  if err!=nil { return nil,err } else {}
  var record InteractionRecord
  if err:=json.Unmarshal([]byte(value.Str),&record);err!=nil { return nil,fmt.Errorf("recording entry %d: %w",i,err) } else { records=append(records,record) }
 }
 return records,nil
}

func (c *Coordinator) handleRecording(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"recording requires POST",http.StatusMethodNotAllowed);return }else{}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request RecordingRequest
 if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { json.NewEncoder(w).Encode(RecordingResponse{Error:err.Error()});return }else{}
 response:=RecordingResponse{ObjectID:request.ObjectID,Records:[]InteractionRecord{}}
 var err error
 if request.Action=="start" {
  var value Value
  value,err=c.ensureUIObject("recording:"+newUUID(),"InteractionLog"); response.ObjectID=value.ObjID
 }else if request.Action=="read" {
  response.Records,err=c.readRecording(request.ObjectID)
  response.Script=recordingSource(response.Records,false)
  response.Test=recordingSource(response.Records,true)
 }else { err=fmt.Errorf("unknown recording action %q",request.Action) }
 if err!=nil {response.Error=err.Error()}else{}
 json.NewEncoder(w).Encode(response)
}

func valueSource(value Value,objects map[string]string) string {
 switch value.Kind {
 case "string":return smalltalkString(value.Str)
 case "number":return value.String()
 case "bool":return value.String()
 case "character":return "$"+string(value.Char)
 case "objid":
  if name,ok:=objects[value.ObjID];ok{return name}else{return "#@"+value.ObjID}
 default:return "nil"
 }
}

func messageSource(receiver,method string,args []Value,objects map[string]string) string {
 if strings.Contains(method,":") {
  parts:=strings.Split(method,":")
  result:=receiver
  for i,part:=range parts[:len(parts)-1] {
   if i<len(args) {result+=" "+part+": "+valueSource(args[i],objects)}else{result+=" "+part+": nil"}
  }
  return result
 }else if len(args)>0{return receiver+" "+method+" "+valueSource(args[0],objects)}else{return receiver+" "+method}
}

func recordingSource(records []InteractionRecord,assertions bool) string {
 var source strings.Builder
 objects:=map[string]string{}
 workspaces:=map[string]string{}
 for i,record:=range records {
  result:=fmt.Sprintf("recordedResult%d",i+1)
  expression:=record.Request.Code
  switch record.Request.Kind {
  case "call":expression=messageSource(valueSource(ObjVal(record.Request.ObjectID),objects),record.Request.Method,record.Request.Args,objects)
  case "promise":expression=promiseExpression(record.Request,objects)
  case "example":expression=valueSource(ObjVal(record.Request.ObjectID),objects)+" run. "+valueSource(ObjVal(record.Request.ObjectID),objects)+" result"
  case "workspace":
   id:=record.Request.ObjectID
   ws,ok:=workspaces[id]
   if !ok {
    ws=fmt.Sprintf("recordedWorkspace%d",len(workspaces)+1);workspaces[id]=ws
    fmt.Fprintf(&source,"%s := ObjectWorkspace new.\n%s target: %s.\n",ws,ws,valueSource(ObjVal(id),objects))
   }else{}
   expression=ws+" evaluate: "+smalltalkString(record.Request.Code)+". "+ws+" result"
  case "eval":
  default:continue
  }
  if assertions && record.Response.Error!="" {
   failed:=fmt.Sprintf("recordedFailed%d",i+1)
   fmt.Fprintf(&source,"%s := false.\n[ %s ] onError: [ :error | %s := true ].\n%s ifFalse: [ 'Expected recorded failure at step %d' signal ].\n",failed,expression,failed,failed,i+1)
  }else{
   fmt.Fprintf(&source,"%s := [ %s ] value.\n",result,expression)
   if assertions {
    expected:=valueSource(record.Response.Result,objects)
    if record.Response.Result.Kind=="objid" {
     if record.ResultClass!="" {fmt.Fprintf(&source,"(%s class = %s) ifFalse: [ 'Wrong result class at step %d' signal ].\n",result,smalltalkString(record.ResultClass),i+1)}else{}
    }else{fmt.Fprintf(&source,"(%s = %s) ifFalse: [ 'Wrong result at step %d' signal ].\n",result,expected,i+1)}
   }else{}
  }
  if record.Response.Result.Kind=="objid" && record.Response.Error=="" {objects[record.Response.Result.ObjID]=result}else{}
 }
 return source.String()
}
