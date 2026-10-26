package main

import (
 "encoding/json"
 "fmt"
 "sync"
 "time"
)

const debugPauseTimeout = 10 * time.Minute

type debugSession struct {
 mu sync.Mutex
 state DebugSnapshot
 commands chan DebugCommand
 method *MethodDef
 definingClass *ClassDef
}

type debugResponder struct{}
func (debugResponder) Respond([]byte) error { return nil }

func (obj *ObjectServer) debugSession() *debugSession {
 obj.debugMu.Lock()
 defer obj.debugMu.Unlock()
 return obj.debug
}

func (obj *ObjectServer) installDebug(session *debugSession) error {
 obj.debugMu.Lock()
 defer obj.debugMu.Unlock()
 if obj.debug!=nil && !obj.debug.finished() { return fmt.Errorf("object %s already has a debug session",obj.id) } else {}
 obj.debug=session
 return nil
}

func (s *debugSession) finished() bool {
 s.mu.Lock()
 defer s.mu.Unlock()
 return s.state.State=="completed" || s.state.State=="aborted" || s.state.State=="expired"
}

func copyDebugValues(values map[string]Value) map[string]Value {
 result:=make(map[string]Value,len(values))
 for name,value:=range values { result[name]=value }
 return result
}

func (s *debugSession) snapshot() DebugSnapshot {
 s.mu.Lock()
 defer s.mu.Unlock()
 result:=s.state
 result.Arguments=copyDebugValues(result.Arguments)
 result.Statements=append([]string{},result.Statements...)
 return result
}

func (obj *ObjectServer) startDebug(request DebugRequest) (DebugSnapshot,error) {
	if err:=validateDebugValues(request.Args);err!=nil { return DebugSnapshot{},err } else {}
 method,owner,ok:=lookupMethod(obj.classDef,request.Method)
 if !ok || obj.foreignBridge!=nil || owner.Foreign!=nil { return DebugSnapshot{},fmt.Errorf("debug %s.%s requires a native method body",obj.classDef.Name,request.Method) } else {}
 if len(request.Args)!=len(method.Params) { return DebugSnapshot{},fmt.Errorf("debug %s expects %d arguments, received %d",request.Method,len(method.Params),len(request.Args)) } else {}
 arguments:=map[string]Value{}
 for i,name:=range method.Params { arguments[name]=request.Args[i] }
 state:=DebugSnapshot{ID:newUUID(),ObjectID:obj.id,Method:request.Method,State:"queued",Arguments:arguments,Result:Nil,Statements:[]string{}}
 for _,node:=range method.Body { state.Statements=append(state.Statements,debugExpression(node)) }
 session:=&debugSession{state:state,commands:make(chan DebugCommand,1),method:method,definingClass:owner}
 if err:=obj.installDebug(session);err!=nil { return DebugSnapshot{},err } else {}
 obj.ensureMailbox()
 req:=MessageRequest{MsgID:newUUID(),ChainID:newUUID(),Method:"__debugRun:",Args:[]Value{StrVal(state.ID)}}
 select {
 case obj.mailbox<-objectNATSCall{req:req,msg:debugResponder{}}: return session.snapshot(),nil
 default:
  session.complete("aborted",Nil,"normal mailbox is full")
  return DebugSnapshot{},fmt.Errorf("debug %s: normal mailbox is full",obj.id)
 }
}

func (s *debugSession) complete(state string,result Value,problem string) {
 s.mu.Lock()
 defer s.mu.Unlock()
 s.state.State=state
 s.state.Result=result
 s.state.Error=problem
 s.state.Revision++
 s.state.Deadline=time.Time{}
}

func (s *debugSession) pause(index int,arguments map[string]Value,result Value,problem string) {
 s.mu.Lock()
 defer s.mu.Unlock()
 s.state.State="paused"
 s.state.Index=index
 s.state.Arguments=copyDebugValues(arguments)
 s.state.Result=result
 s.state.Error=problem
 s.state.Revision++
 s.state.Deadline=time.Now().Add(debugPauseTimeout)
}

func (s *debugSession) submit(command DebugCommand) error {
 s.mu.Lock()
 defer s.mu.Unlock()
 if command.ID!=s.state.ID || command.Revision!=s.state.Revision || s.state.State!="paused" { return fmt.Errorf("debug frame changed; refresh before submitting a command") } else {}
 select {
 case s.commands<-command:
  for name,value:=range command.Arguments { s.state.Arguments[name]=value }
  s.state.State="running";return nil
 default:return fmt.Errorf("debugger already has a pending command")
 }
}

func (obj *ObjectServer) debugCommand(command DebugCommand) (DebugSnapshot,error) {
 session:=obj.debugSession()
 if session==nil { return DebugSnapshot{},fmt.Errorf("object %s has no debug session",obj.id) } else {}
 switch command.Action {
 case "step","continue","retry","abort","return":
 default:return DebugSnapshot{},fmt.Errorf("unknown debug action %q",command.Action)
 }
 if command.Action=="abort" { command.Code="";command.Arguments=nil;command.Fields=nil } else {}
 if command.Value.Kind=="" { command.Value=Nil } else {}
 values:=[]Value{command.Value}
 for _,value:=range command.Arguments { values=append(values,value) }
 for _,value:=range command.Fields { values=append(values,value) }
 if err:=validateDebugValues(values);err!=nil { return session.snapshot(),err } else {}
 if command.Code!="" {
  if nodes,err:=ParseSmalltalk(command.Code);err!=nil { return session.snapshot(),err } else if len(nodes)==0 { return session.snapshot(),fmt.Errorf("replacement statement is empty") } else {}
 } else {}
 snapshot:=session.snapshot()
 for name:=range command.Arguments {
  if _,ok:=snapshot.Arguments[name];!ok { return snapshot,fmt.Errorf("unknown method argument %q",name) } else {}
 }
 for name:=range command.Fields {
  if !classAllowsSetVar(obj.classDef,name) { return snapshot,fmt.Errorf("unknown receiver field %q",name) } else {}
 }
 if err:=session.submit(command);err!=nil { return session.snapshot(),err } else { return session.snapshot(),nil }
}

func (obj *ObjectServer) runDebug(id string) Value {
 session:=obj.debugSession()
 if session==nil || session.snapshot().ID!=id { return Nil } else {}
 ctx:=&EvalCtx{obj:obj,args:copyDebugValues(session.snapshot().Arguments),definingClass:session.definingClass}
 result:=obj.debugBody(session,ctx)
 return result
}

func (obj *ObjectServer) debugBody(session *debugSession,ctx *EvalCtx) (result Value) {
 result=Nil
 defer func(){
  if recovered:=recover();recovered!=nil {
   if signal,ok:=recovered.(returnSignal);ok { result=signal.value;session.complete("completed",result,"") } else { panic(recovered) }
  } else {}
 }()
 stepping:=true
 for index,node:=range session.method.Body {
  var stopped bool
  result,stepping,stopped=obj.debugStatement(session,ctx,index,node,result,stepping)
  if stopped { return result } else {}
 }
 session.complete("completed",result,"")
 return result
}

func (obj *ObjectServer) debugStatement(session *debugSession,ctx *EvalCtx,index int,node *Node,previous Value,stepping bool) (Value,bool,bool) {
 nodes:=[]*Node{node}
 problem:=""
 for {
  if stepping || problem!="" {
   session.pause(index,ctx.args,previous,problem)
   command:=waitDebugCommand(session)
   if command.Action=="abort" { session.complete("aborted",previous,problem);return previous,false,true } else if command.Action=="expired" { session.complete("expired",previous,"debug pause expired after ten minutes");return previous,false,true } else {}
   obj.applyDebugValues(ctx,command)
   if command.Action=="return" { session.complete("completed",command.Value,"");return command.Value,false,true } else {}
   if command.Code!="" { nodes,_=ParseSmalltalk(command.Code) } else {}
   stepping=command.Action!="continue"
  } else {}
  reply:=dispatchToReply("",func() Value {
   value:=Nil
   for _,expr:=range nodes { value=ctx.eval(expr) }
   return value
  })
  if reply.Signaled { problem=reply.SignalMsg } else { return reply.Result,stepping,false }
 }
}

func validateDebugValues(values []Value) error {
 body,err:=json.Marshal(MCPToolArguments{Args:values})
 if err!=nil { return err } else {}
 var decoded MCPToolArguments
 return decodeMCPArguments(body,&decoded)
}

func waitDebugCommand(session *debugSession) DebugCommand {
 timer:=time.NewTimer(debugPauseTimeout)
 defer timer.Stop()
 select {
 case command:=<-session.commands:return command
 case <-timer.C:return DebugCommand{Action:"expired"}
 }
}

func (obj *ObjectServer) applyDebugValues(ctx *EvalCtx,command DebugCommand) {
 for name,value:=range command.Arguments { ctx.args[name]=value }
 obj.mu.Lock()
 defer obj.mu.Unlock()
 for name,value:=range command.Fields { obj.instanceVars[name]=value }
}

func (obj *ObjectServer) debugDispatch(name string,args []Value) Value {
 var state DebugSnapshot
 var err error
 switch name {
 case "__debugStart:":
  var request DebugRequest
  if len(args)!=1 { err=fmt.Errorf("debug start requires a JSON request") } else if err=json.Unmarshal([]byte(args[0].Str),&request);err==nil { state,err=obj.startDebug(request) } else {}
 case "__debugCommand:":
  var command DebugCommand
  if len(args)!=1 { err=fmt.Errorf("debug command requires a JSON command") } else if err=json.Unmarshal([]byte(args[0].Str),&command);err==nil { state,err=obj.debugCommand(command) } else {}
 case "__debugJson":
  if session:=obj.debugSession();session!=nil { state=session.snapshot() } else { state.State="idle" }
 case "__debugRun:":
  if len(args)==1 { return obj.runDebug(args[0].Str) } else { err=fmt.Errorf("debug execution requires a session ID") }
 }
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 body,_:=json.Marshal(state)
 return StrVal(string(body))
}

func (obj *ObjectServer) debugReferences() []string {
 session:=obj.debugSession()
 if session==nil { return nil } else {}
 snapshot:=session.snapshot()
 refs:=[]string{}
 for _,value:=range snapshot.Arguments { if value.Kind=="objid" { refs=append(refs,value.ObjID) } else {} }
 if snapshot.Result.Kind=="objid" { refs=append(refs,snapshot.Result.ObjID) } else {}
 return refs
}
