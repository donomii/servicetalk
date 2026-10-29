package main

import (
 "encoding/json"
 "sort"
 "strconv"
 "strings"
)

func (obj *ObjectServer) collaborationFields(value Value) map[string]Value {
 if value.Kind!="objid" { panic(signalError{msg:"Collaboration state requires an object reference"}) } else {}
 reply:=obj.sendMessage(value,"__inspectJson",nil)
 var inspection InspectResponse
 if err:=json.Unmarshal([]byte(reply.Str),&inspection);err!=nil { panic(signalError{msg:"Cannot read collaboration object: "+err.Error()}) } else { return inspection.InstanceVars }
}

func (obj *ObjectServer) collaborationElements(value Value) []Value {
 fields:=obj.collaborationFields(value)
 elements:=make([]Value,len(fields))
 for key,item:=range fields {
  index,err:=strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key,"["),"]"))
  if err!=nil || index<0 || index>=len(elements) { panic(signalError{msg:"Invalid collaboration array index: "+key}) } else { elements[index]=item }
 }
 return elements
}

func (obj *ObjectServer) collaborationSnapshot() Value {
 fields:=obj.inspectResponse().InstanceVars
 response:=SessionResponse{SessionID:obj.id,Participants:[]SessionParticipantDescription{},Publications:[]SessionPublication{},Activity:[]SessionActivity{},Sequence:int64(fields["sequence"].Num)}
 names:=map[string]string{}
 for _,reference:=range obj.collaborationElements(fields["participants"]) {
  item:=obj.collaborationFields(reference)
  participant:=SessionParticipantDescription{ID:reference.ObjID,Name:item["name"].Str,Kind:item["kind"].Str,Active:item["active"].Bool,WorkspaceID:item["workspace"].ObjID}
  response.Participants=append(response.Participants,participant);names[participant.ID]=participant.Name
 }
 for name,reference:=range obj.collaborationFields(fields["board"]) {
  item:=obj.collaborationFields(reference);author:=item["participant"].ObjID
  response.Publications=append(response.Publications,SessionPublication{Name:name,Value:item["value"],Revision:int64(item["revision"].Num),ParticipantID:author,ParticipantName:names[author],Present:item["present"].Bool})
 }
 sort.Slice(response.Publications,func(i,j int)bool{return response.Publications[i].Name<response.Publications[j].Name})
 for _,reference:=range obj.collaborationElements(fields["activity"]) {
  pair:=obj.collaborationElements(reference)
  if len(pair)!=2 { panic(signalError{msg:"Invalid collaboration activity entry"}) } else {}
  var event SessionActivity
  if err:=json.Unmarshal([]byte(pair[1].Str),&event);err!=nil { panic(signalError{msg:"Cannot read collaboration activity: "+err.Error()}) } else {}
  event.Sequence=int64(pair[0].Num);response.Activity=append(response.Activity,event)
 }
 data,err:=json.Marshal(response)
 if err!=nil { panic(signalError{msg:"Cannot encode collaboration snapshot: "+err.Error()}) } else { return StrVal(string(data)) }
}
