package main

import (
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net/http"
 "strings"
 "time"
 "unicode"
)

func smalltalkString(text string) string { return "'" + strings.ReplaceAll(text, "'", "''") + "'" }

func (c *Coordinator) ensureUIObject(key, class string) (Value, error) {
 ws, err := c.ensureWorkspace()
 if err != nil { return Nil, err } else {}
 name := "__ui_" + hex.EncodeToString([]byte(key))
 source := fmt.Sprintf("%s isNil ifTrue: [ %s := %s new ] ifFalse: [ nil ]. %s", name, name, class, name)
 return c.callObjectID(ws,"evalInternal:",[]Value{StrVal(source)},evalTimeout())
}

func (c *Coordinator) retainUIResult(value Value) (string, error) {
 ws, err := c.ensureWorkspace()
 if err != nil { return "", err } else {}
 handle := "result_" + newUUID()
 _, err = c.callObjectID(ws,"setVar:to:",[]Value{StrVal(handle),value},inspectionTimeout)
 return handle, err
}

func (c *Coordinator) runInteraction(request InteractionRequest) (WorkspaceResult, error) {
 switch request.Kind {
 case "eval":
  ws, err := c.ensureWorkspace()
  if err != nil { return WorkspaceResult{}, err } else {}
  value, err := c.callObjectID(ws,"evalResult:",[]Value{StrVal(request.Code)},evalTimeout())
  if err != nil { return WorkspaceResult{}, err } else {}
  var result WorkspaceResult
  err = json.Unmarshal([]byte(value.Str),&result)
  return result, err
 case "workspace": return c.evaluateObjectWorkspace(request.ObjectID,request.Code)
 case "example":
  value,err:=c.callObjectID(request.ObjectID,"run",nil,evalTimeout())
  if err!=nil { return WorkspaceResult{},err } else {}
  var result WorkspaceResult
  err=json.Unmarshal([]byte(value.Str),&result)
  return result,err
 case "call", "promise":
  raw, err := json.Marshal(MCPToolArguments{ObjectID:request.ObjectID,Method:request.Method,Args:request.Args})
  if err != nil { return WorkspaceResult{}, err } else if err = validateMCPArguments("call",raw); err != nil { return WorkspaceResult{}, err } else {}
  if request.Kind=="promise" {
   if request.TimeoutMs!=0 { if _,ok:=messageTimeoutFromMilliseconds(NumVal(request.TimeoutMs));!ok { return WorkspaceResult{},fmt.Errorf("invalid promise timeout") } else {} } else {}
   ws,err:=c.ensureWorkspace()
   if err!=nil { return WorkspaceResult{},err } else {}
   value,err:=c.callObjectID(ws,"evalInternal:",[]Value{StrVal(promiseExpression(request,nil))},evalTimeout())
   return WorkspaceResult{Result:value},err
  } else {}
  value, err := c.callObjectID(request.ObjectID,request.Method,request.Args,messageCallTimeout(request.Method,request.Args))
  return WorkspaceResult{Result:value},err
 default: return WorkspaceResult{},fmt.Errorf("unknown interaction kind %q",request.Kind)
 }
}

func (c *Coordinator) interact(request InteractionRequest) InteractionResult {
 result := InteractionResult{Result:Nil}
 value, err := c.runInteraction(request)
 if err != nil { result.Error=err.Error() } else {
  result.Result=value.Result; result.Output=value.Output
  result.Handle, err=c.retainUIResult(value.Result)
  if err != nil { result.Error="operation completed, but retaining its result failed: "+err.Error() } else {}
 }
 if request.RecordingID!="" {
  record := InteractionRecord{Request:request,Response:result,At:time.Now()}
  record.Request.RecordingID=""
  if result.Result.Kind=="objid" {
   if class, err:=c.callObjectID(result.Result.ObjID,"class",nil,inspectionTimeout); err==nil { record.ResultClass=class.Str } else {}
  } else {}
  data, err:=json.Marshal(record)
  if err==nil { _,err=c.callObjectID(request.RecordingID,"append:",[]Value{StrVal(string(data))},inspectionTimeout) } else {}
  if err==nil { err=c.retainRecordingReferences(request,result.Result) } else {}
  if err!=nil { result.RecordingError=err.Error() } else {}
 }
 return result
}

func (c *Coordinator) handleInteraction(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"interaction requires POST",http.StatusMethodNotAllowed); return } else {}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request InteractionRequest
 if err:=json.NewDecoder(r.Body).Decode(&request); err!=nil { json.NewEncoder(w).Encode(InteractionResult{Error:err.Error()}) } else { json.NewEncoder(w).Encode(c.interact(request)) }
}

func (c *Coordinator) handleResult(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"result requires POST",http.StatusMethodNotAllowed); return } else {}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request ResultRequest
 if err:=json.NewDecoder(r.Body).Decode(&request); err!=nil { json.NewEncoder(w).Encode(InteractionResult{Error:err.Error()}); return } else {}
 result:=InteractionResult{Result:Nil}
 ws,err:=c.ensureWorkspace()
 if err==nil { err=c.changeResult(ws,request) } else {}
 if err!=nil { result.Error=err.Error() } else {}
 json.NewEncoder(w).Encode(result)
}

func (c *Coordinator) changeResult(ws string,request ResultRequest) error {
 if !strings.HasPrefix(request.Handle,"result_") || !validBindingName(request.Handle) { return fmt.Errorf("invalid result handle %q",request.Handle) } else {}
 switch request.Action {
 case "release":
  _,err:=c.callObjectID(ws,"setVar:to:",[]Value{StrVal(request.Handle),Nil},inspectionTimeout)
  return err
 case "bind":
  if !validBindingName(request.Name) || strings.HasPrefix(request.Name,"__") { return fmt.Errorf("invalid binding name %q",request.Name) } else {}
  value,err:=c.callObjectID(ws,"getVar:",[]Value{StrVal(request.Handle)},inspectionTimeout)
  if err!=nil { return err } else {}
  _,err=c.callObjectID(ws,"setVar:to:",[]Value{StrVal(request.Name),value},inspectionTimeout)
  return err
 default: return fmt.Errorf("unknown result action %q",request.Action)
 }
}

func validBindingName(name string) bool {
 if name=="" { return false } else {}
 for i,ch:=range name {
  if unicode.IsLetter(ch) || ch=='_' || (i>0 && unicode.IsDigit(ch)) { continue } else { return false }
 }
 switch name { case "self","super","true","false","nil","coordinator": return false; default:return true }
}

func (c *Coordinator) retainRecordingReferences(request InteractionRequest,result Value) error {
 values:=append([]Value{result},request.Args...)
 if request.ObjectID!="" {values=append(values,ObjVal(request.ObjectID))}else{}
 for _,value:=range values {
  if value.Kind=="objid" {
   if _,err:=c.callObjectID(request.RecordingID,"retain:",[]Value{value},inspectionTimeout);err!=nil{return err}else{}
  }else{}
 }
 return nil
}
