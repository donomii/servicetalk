package main

import (
 "encoding/json"
 "fmt"
 "net/http"
)

func (c *Coordinator) debugRequest(request DebugHTTPRequest) DebugHTTPResponse {
 response:=DebugHTTPResponse{}
 method:="__debugJson"
 args:=[]Value{}
 switch request.Action {
 case "start":
  body,err:=json.Marshal(MCPToolArguments{ObjectID:request.ObjectID,Method:request.Method,Args:request.Args})
  if err==nil { err=validateMCPArguments("call",body) } else {}
  if err!=nil { response.Error=err.Error();return response } else {}
  body,_=json.Marshal(request.DebugRequest)
  method="__debugStart:";args=[]Value{StrVal(string(body))}
 case "command":
  body,_:=json.Marshal(request.Command)
  method="__debugCommand:";args=[]Value{StrVal(string(body))}
 case "read":
 default:response.Error=fmt.Sprintf("unknown debugger action %q",request.Action);return response
 }
 value,err:=c.callObjectID(request.ObjectID,method,args,inspectionTimeout)
 if err==nil { err=json.Unmarshal([]byte(value.Str),&response.Snapshot) } else {}
 if err!=nil { response.Error=err.Error() } else {}
 return response
}

func (c *Coordinator) handleDebugger(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"debugger requires POST",http.StatusMethodNotAllowed);return } else {}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request DebugHTTPRequest
 if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { json.NewEncoder(w).Encode(DebugHTTPResponse{Error:err.Error()}) } else { json.NewEncoder(w).Encode(c.debugRequest(request)) }
}
