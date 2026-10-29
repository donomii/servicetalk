package main

import (
 "encoding/hex"
 "encoding/json"
 "fmt"
 "net/http"
 "strings"
 "time"
)

func (c *Coordinator) collaborationSession() (string,error) {
 ws,err:=c.ensureWorkspace()
 if err!=nil { return "",err } else {}
 snapshot,err:=c.inspectObject(ws,false)
 if err!=nil { return "",err } else {}
 if existing:=snapshot.InstanceVars["__ui_"+hex.EncodeToString([]byte("collaboration"))];existing.Kind=="objid" { return existing.ObjID,nil } else {}
 value,err:=c.ensureUIObject("collaboration","CollaborationSession")
 return value.ObjID,err
}

func (c *Coordinator) sessionSnapshot(id string) (SessionResponse,error) {
 value,err:=c.callObjectID(id,"snapshot",nil,evalTimeout())
 if err!=nil { return SessionResponse{},err } else {}
 var response SessionResponse
 err=json.Unmarshal([]byte(value.Str),&response)
 return response,err
}

func (c *Coordinator) participantWorkspace(id string) (string,error) {
 if id=="" { return c.ensureWorkspace() } else {}
 session,err:=c.collaborationSession()
 if err!=nil { return "",err } else {}
 if _,err=c.callObjectID(session,"activeParticipant:",[]Value{ObjVal(id)},inspectionTimeout);err!=nil { return "",err } else {}
 workspace,err:=c.callObjectID(id,"workspace",nil,inspectionTimeout)
 if err!=nil { return "",err } else if workspace.Kind!="objid" { return "",fmt.Errorf("participant %s has no active workspace",id) } else { return workspace.ObjID,nil }
}

func (c *Coordinator) sessionRecord(session,participant,action,detail string) error {
 name,err:=c.callObjectID(participant,"name",nil,inspectionTimeout)
 if err!=nil { return err } else {}
 event:=SessionActivity{ParticipantID:participant,Name:name.Str,Action:action,Detail:detail,At:time.Now().UTC()}
 data,err:=json.Marshal(event)
 if err!=nil { return err } else {}
 _,err=c.callObjectID(session,"record:",[]Value{StrVal(string(data))},inspectionTimeout)
 return err
}

func validateSessionRequest(request SessionRequest) error {
 switch request.Action {
 case "status":return nil
 case "join":
  if strings.TrimSpace(request.Name)=="" || len(request.Name)>80 { return fmt.Errorf("participant name must contain 1 to 80 bytes") } else if request.Kind!="human" && request.Kind!="agent" { return fmt.Errorf("participant kind must be human or agent") } else { return nil }
 case "publish","unpublish","bind":
  if strings.TrimSpace(request.Name)=="" || len(request.Name)>120 { return fmt.Errorf("publication name must contain 1 to 120 bytes") } else {}
  if request.Action=="bind" {
   if !validBindingName(request.Binding) || strings.HasPrefix(request.Binding,"__") { return fmt.Errorf("invalid publication binding %q",request.Binding) } else {}
  } else if request.ExpectedRevision==nil || *request.ExpectedRevision<0 || *request.ExpectedRevision>9007199254740991 { return fmt.Errorf("publish and unpublish require a nonnegative expectedRevision") } else {}
  if request.Action=="publish" {
   if request.Value==nil { return fmt.Errorf("publish requires a typed value") } else if err:=validateDebugValues([]Value{*request.Value});err!=nil { return err } else {}
  } else {}
 case "leave":
 case "note":
  if strings.TrimSpace(request.Text)=="" || len(request.Text)>2000 { return fmt.Errorf("session note must contain 1 to 2000 bytes") } else {}
 default:return fmt.Errorf("unknown session action %q",request.Action)
 }
 if request.ParticipantID=="" { return fmt.Errorf("%s requires participantId",request.Action) } else { return nil }
}

func (c *Coordinator) sessionAction(id string,request SessionRequest) (string,error) {
 participant:=request.ParticipantID
 if request.Action=="status" { return participant,nil } else if request.Action=="join" {
  value,err:=c.callObjectID(id,"joinName:kind:",[]Value{StrVal(request.Name),StrVal(request.Kind)},evalTimeout())
  if err!=nil { return "",err } else { participant=value.ObjID }
 } else {
  if _,err:=c.participantWorkspace(participant);err!=nil { return participant,err } else {}
  var method string;var args []Value
  switch request.Action {
  case "leave":method="leave:";args=[]Value{ObjVal(participant)}
  case "publish":
   if request.Value.Kind=="objid" { if _,err:=c.inspectObject(request.Value.ObjID,false);err!=nil { return participant,err } else {} } else {}
   method="publish:value:expected:by:";args=[]Value{StrVal(request.Name),*request.Value,NumVal(float64(*request.ExpectedRevision)),ObjVal(participant)}
  case "unpublish":method="unpublish:expected:by:";args=[]Value{StrVal(request.Name),NumVal(float64(*request.ExpectedRevision)),ObjVal(participant)}
  case "bind":
   if err:=c.bindPublication(id,request);err!=nil { return participant,err } else { return participant,c.sessionRecord(id,participant,request.Action,request.Name) }
  case "note":return participant,c.sessionRecord(id,participant,"note",request.Text)
  }
  if _,err:=c.callObjectID(id,method,args,evalTimeout());err!=nil { return participant,err } else {}
 }
 return participant,c.sessionRecord(id,participant,request.Action,request.Name)
}

func (c *Coordinator) bindPublication(session string,request SessionRequest) (resultErr error) {
 transfer,err:=c.callObjectID(session,"prepareBinding:for:",[]Value{StrVal(request.Name),ObjVal(request.ParticipantID)},inspectionTimeout)
 if err!=nil { return err } else {}
 defer func(){if _,releaseErr:=c.callObjectID(session,"releaseBinding:",[]Value{transfer},inspectionTimeout);releaseErr!=nil { resultErr=fmt.Errorf("binding error: %v; transfer release failed: %w",resultErr,releaseErr) } else {}}()
 snapshot,err:=c.inspectObject(transfer.ObjID,false)
 if err!=nil { return err } else {}
 value,workspace:=snapshot.InstanceVars["[0]"],snapshot.InstanceVars["[1]"]
 if workspace.Kind!="objid" { return fmt.Errorf("participant %s transfer has no workspace",request.ParticipantID) } else {}
 _,err=c.callObjectID(workspace.ObjID,"setVar:to:",[]Value{StrVal(request.Binding),value},evalTimeout())
 return err
}

func (c *Coordinator) runSession(request SessionRequest) SessionResponse {
 response:=SessionResponse{Participants:[]SessionParticipantDescription{},Publications:[]SessionPublication{},Activity:[]SessionActivity{}}
 if request.Action=="" { request.Action="status" } else {}
 if request.Action=="join" && request.Kind=="" { request.Kind="human" } else {}
 request.Name=strings.TrimSpace(request.Name)
 if err:=validateSessionRequest(request);err!=nil { response.Error=err.Error();return response } else {}
 session,err:=c.collaborationSession()
 if err!=nil { response.Error=err.Error();return response } else {}
 participant,actionErr:=c.sessionAction(session,request)
 response,err=c.sessionSnapshot(session)
 if err!=nil { response.Error=err.Error();return response } else {}
 response.ParticipantID=participant
 for _,item:=range response.Participants { if item.ID==participant { response.WorkspaceID=item.WorkspaceID } else {} }
 if actionErr!=nil { response.Error=actionErr.Error() } else {}
 return response
}

func (c *Coordinator) handleSession(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 request:=SessionRequest{Action:"status",ParticipantID:r.URL.Query().Get("participantId")}
 if r.Method==http.MethodPost {
  r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  if err:=decoder.Decode(&request);err!=nil { json.NewEncoder(w).Encode(SessionResponse{Error:err.Error()});return } else {}
 } else if r.Method!=http.MethodGet { http.Error(w,"session requires GET or POST",http.StatusMethodNotAllowed);return } else {}
 json.NewEncoder(w).Encode(c.runSession(request))
}
