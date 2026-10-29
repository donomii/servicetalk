package main

import (
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net/http"
 "strings"
)

func objectWorkspaceExpression(id, source string) string {
 name := "__inspectorWorkspace_" + hex.EncodeToString([]byte(id))
 return fmt.Sprintf("%s isNil ifTrue: [ %s := ObjectWorkspace new. %s target: #@%s ] ifFalse: [ nil ]. %s evaluate: '%s'", name, name, name, id, name, strings.ReplaceAll(source, "'", "''"))
}

func (c *Coordinator) evaluateObjectWorkspace(id, source string) (WorkspaceResult, error) {
 ws,err:=c.ensureWorkspace()
 if err!=nil { return WorkspaceResult{},err } else { return c.evaluateObjectWorkspaceIn(ws,id,source) }
}

func (c *Coordinator) evaluateObjectWorkspaceIn(ws,id, source string) (WorkspaceResult, error) {
 for _, ch := range id {
  if !isObjIdChar(ch) { return WorkspaceResult{}, fmt.Errorf("invalid receiver object ID %q", id) } else {}
 }
 if _, err := c.inspectObject(id, false); err != nil {
  return WorkspaceResult{}, fmt.Errorf("open workspace for %s: %w", id, err)
 } else { /* Confirm the receiver exists before creating a rooted workspace. */ }
 reply, err := c.callObjectID(ws, "evalInternal:", []Value{StrVal(objectWorkspaceExpression(id, source))}, evalTimeout())
 if err != nil { return WorkspaceResult{}, err } else {}
 var result WorkspaceResult
 if err := json.Unmarshal([]byte(reply.Str), &result); err != nil {
  return WorkspaceResult{}, fmt.Errorf("decode object workspace result: %w", err)
 } else { return result, nil }
}

func (c *Coordinator) handleObjectWorkspace(w http.ResponseWriter, r *http.Request) {
 w.Header().Set("Content-Type", "application/json")
 if r.Method != http.MethodPost { http.Error(w,"object workspace requires POST",http.StatusMethodNotAllowed); return } else {}
 r.Body = http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request ObjectWorkspaceRequest
 if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
  w.WriteHeader(http.StatusBadRequest)
  json.NewEncoder(w).Encode(WorkspaceResult{Error: "decode object workspace request: " + err.Error()})
 } else if request.ObjectID == "" {
  w.WriteHeader(http.StatusBadRequest)
  json.NewEncoder(w).Encode(WorkspaceResult{Error: "object workspace requires objectId"})
 } else if result, err := c.evaluateObjectWorkspace(request.ObjectID, request.Code); err != nil {
  json.NewEncoder(w).Encode(WorkspaceResult{Error: err.Error()})
 } else { json.NewEncoder(w).Encode(result) }
}
