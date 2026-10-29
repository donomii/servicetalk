package main

import (
 "encoding/json"
 "fmt"
 "time"
)

func serviceNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func serviceTargetKey(id string) string { return "__serviceTarget_"+id }

// serviceStateUpdate serializes short state transitions; callbacks only inspect or change data.
func (obj *ObjectServer) serviceStateUpdate(change func(*ServiceStatus,map[string]Value) error) error {
 obj.mu.Lock()
 defer obj.mu.Unlock()
 state:=ServiceStatus{Generations:[]ServiceGeneration{}}
 if raw:=obj.instanceVars["serviceState"];raw.Kind=="string" && raw.Str!="" {
  if err:=json.Unmarshal([]byte(raw.Str),&state);err!=nil { return fmt.Errorf("Service state is invalid: %w",err) } else {}
 } else {}
 if err:=change(&state,obj.instanceVars);err!=nil { return err } else {}
 raw,err:=json.Marshal(state)
 if err!=nil { return fmt.Errorf("encode Service state: %w",err) } else { obj.instanceVars["serviceState"]=StrVal(string(raw));return nil }
}

func serviceGeneration(state *ServiceStatus,id string) (*ServiceGeneration,error) {
 for i:=range state.Generations { if state.Generations[i].ID==id { return &state.Generations[i],nil } else {} }
 return nil,fmt.Errorf("Service generation %q does not exist",id)
}

func serviceDrain(generation *ServiceGeneration,fields map[string]Value) {
 if generation.State=="draining" && generation.InFlight==0 && generation.Uncertain==0 {
  generation.State="retired";generation.RetiredAt=serviceNow();delete(fields,serviceTargetKey(generation.ID))
 } else {}
}

func (obj *ObjectServer) serviceReserve(className string) (string,error) {
 id:=""
 err:=obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  state.Next++;id=fmt.Sprintf("g%d",state.Next)
  state.Generations=append(state.Generations,ServiceGeneration{ID:id,ClassName:className,State:"preparing",PreparedAt:serviceNow()})
  return nil
 })
 return id,err
}

func (obj *ObjectServer) servicePrepared(id,className,version,target,failure string) error {
 return obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else {}
  generation.ClassName=className;generation.Version=version;generation.Target=target;generation.Error=failure
  if target!="" { fields[serviceTargetKey(id)]=ObjVal(target) } else {}
  if failure!="" { generation.State="failed" } else {}
  return nil
 })
}

func (obj *ObjectServer) serviceInitialized(id string,uncertain bool,failure string) error {
 return obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else {}
  if uncertain { generation.Uncertain++ } else {}
  if failure=="" { generation.State="candidate" } else { generation.State="failed";generation.Error=failure }
  return nil
 })
}

func (obj *ObjectServer) serviceBegin(id string,check bool) (ServiceGeneration,error) {
 result:=ServiceGeneration{}
 err:=obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  if !check { id=state.Current } else {}
  if id=="" { return fmt.Errorf("Service has no active generation; prepare, check and promote one first") } else {}
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else {}
  if check && generation.State!="candidate" && generation.State!="ready" && generation.State!="failed" { return fmt.Errorf("cannot check generation %s in state %s",id,generation.State) } else if !check && generation.State!="active" { return fmt.Errorf("cannot route to generation %s in state %s",id,generation.State) } else {}
  if fields[serviceTargetKey(id)].Kind!="objid" { return fmt.Errorf("generation %s has no prepared target",id) } else {}
  if check && generation.Uncertain>0 { return fmt.Errorf("generation %s has %d calls without replies; inspect it or force retirement",id,generation.Uncertain) } else {}
  if check { generation.State="checking" } else {}
  generation.InFlight++;result=*generation
  return nil
 })
 return result,err
}

func (obj *ObjectServer) serviceFinish(id string,uncertain,check bool,failure string) error {
 return obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else {}
  generation.InFlight--
  if uncertain && generation.State!="forced" { generation.Uncertain++ } else {}
  if failure!="" { generation.Error=failure } else if check { generation.Error="" } else {}
  if check && generation.State!="forced" {
   if failure=="" { generation.State="ready" } else { generation.State="failed" }
  } else {}
  serviceDrain(generation,fields)
  return nil
 })
}

func (obj *ObjectServer) servicePromote(id string) error {
 return obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  next,err:=serviceGeneration(state,id)
  if err!=nil { return err } else if next.State!="ready" { return fmt.Errorf("generation %s is %s; a successful check is required before promotion",id,next.State) } else {}
  if state.Current!="" {
   old,err:=serviceGeneration(state,state.Current)
   if err!=nil { return err } else { old.State="draining";serviceDrain(old,fields) }
  } else {}
  next.State="active";next.PromotedAt=serviceNow();state.Current=id
  return nil
 })
}

func (obj *ObjectServer) serviceRetire(id,operation string) (string,error) {
 target:=""
 err:=obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else if state.Current==id { return fmt.Errorf("cannot %s active generation %s; promote its replacement first",operation,id) } else {}
  if generation.State=="preparing" || generation.State=="checking" { return fmt.Errorf("cannot %s generation %s while %s",operation,id,generation.State) } else {}
  if operation=="force" {
   target=generation.Target
   if target=="" { return fmt.Errorf("generation %s has no target to force retire",id) } else {}
   if generation.State!="retired" && generation.State!="forced" { generation.State="draining" } else {}
  } else if operation=="discard" && generation.State!="candidate" && generation.State!="ready" && generation.State!="failed" { return fmt.Errorf("discard requires a candidate, ready or failed generation; %s is %s",id,generation.State) } else {
   if generation.State!="forced" && generation.State!="retired" { generation.State="draining";serviceDrain(generation,fields) } else {}
  }
  return nil
 })
 return target,err
}

func (obj *ObjectServer) serviceForced(id,failure string) error {
 return obj.serviceStateUpdate(func(state *ServiceStatus,fields map[string]Value)error{
  generation,err:=serviceGeneration(state,id)
  if err!=nil { return err } else {}
  if failure!="" { generation.Error=failure } else {
   generation.State="forced";generation.Uncertain=0;generation.RetiredAt=serviceNow();delete(fields,serviceTargetKey(id))
  }
  return nil
 })
}
