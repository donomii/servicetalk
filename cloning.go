package main

import (
 "bytes"
 "crypto/sha256"
 "encoding/json"
 "fmt"
 "os"
 "strings"
)

const snapshotInitClass = "__native_snapshot__"

func snapshotDefinition(class *ClassDef) *NativeDefinition {
 if class==nil { return nil } else {}
 methods:=map[string]*MethodDef{}
 for name,method:=range class.Methods { methods[name]=method }
 return &NativeDefinition{Name:class.Name,Vars:append([]string{},class.Vars...),Methods:methods,Superclass:snapshotDefinition(class.Superclass)}
}

func restoreDefinition(definition *NativeDefinition) *ClassDef {
 if definition==nil { return nil } else {}
 class:=&ClassDef{Name:definition.Name,Vars:definition.Vars,Methods:definition.Methods,Superclass:restoreDefinition(definition.Superclass)}
 if class.Superclass!=nil { class.SuperclassName=class.Superclass.Name } else {}
 return class
}

func definitionVersion(definition *NativeDefinition) string {
 body,_:=json.Marshal(definition)
 return fmt.Sprintf("%x",sha256.Sum256(body))
}

func cloneableClass(class *ClassDef) bool {
 if class==nil { return false } else {}
 for current:=class;current!=nil;current=current.Superclass { if current.Foreign!=nil { return false } else {} }
 switch class.Name {
 case BuiltinClassProcess,BuiltinClassHttpServer,BuiltinClassRobotsMatcher,BuiltinClassSitemapParser:return false
 case BuiltinClassWorkspace,BuiltinClassArray,BuiltinClassDictionary,BuiltinClassBlock,BuiltinClassHttpClient:return true
 default:return !strings.HasPrefix(class.Name,"__")
 }
}

func (obj *ObjectServer) nativeSnapshot() (NativeSnapshot,error) {
 if !cloneableClass(obj.classDef) || obj.foreignBridge!=nil { return NativeSnapshot{},fmt.Errorf("%s owns runtime-specific state; implement clone to copy it",obj.classDef.Name) } else {}
 snapshot:=NativeSnapshot{Definition:snapshotDefinition(obj.classDef),Prototype:obj.id,Block:obj.blockDef}
 promise:=obj.isPromise()
 obj.mu.RLock()
 defer obj.mu.RUnlock()
 if promise && obj.instanceVars["state"].Str=="pending" { return NativeSnapshot{},fmt.Errorf("a pending Promise cannot be cloned; await its outcome first") } else {}
 snapshot.Fields=copyDebugValues(obj.instanceVars)
 snapshot.Array=append([]Value{},obj.arrayData...)
 snapshot.Dictionary=copyDebugValues(obj.dictionaryData)
 return snapshot,nil
}

func applyPrototypePatch(snapshot *NativeSnapshot,patch PrototypePatch) error {
 class:=restoreDefinition(snapshot.Definition)
 for name,value:=range patch.Fields {
  if !classAllowsSetVar(class,name) { return fmt.Errorf("prototype field %q is not declared",name) } else if err:=validateDebugValues([]Value{value});err!=nil { return err } else { snapshot.Fields[name]=value }
 }
 if strings.HasPrefix(class.Name,"__") && len(patch.Methods)>0 { return fmt.Errorf("method derivation requires a native user-defined object") } else {}
 for _,method:=range patch.Methods {
  if err:=validatePrototypeMethod(method);err!=nil { return err } else {}
  body,err:=ParseSmalltalk(method.Source)
  if err!=nil { return fmt.Errorf("prototype method %s: %w",method.Name,err) } else {}
  snapshot.Definition.Methods[method.Name]=&MethodDef{Name:method.Name,Params:append([]string{},method.Params...),Body:body}
 }
 return nil
}

func validatePrototypeMethod(method PrototypeMethod) error {
 if method.Name=="" { return fmt.Errorf("prototype method needs a selector") } else {}
 count:=strings.Count(method.Name,":")
 if count==0 && !validBindingName(method.Name) { count=1 } else {}
 if count!=len(method.Params) { return fmt.Errorf("prototype method %s expects %d parameter names",method.Name,count) } else {}
 names:=map[string]bool{}
 for _,name:=range method.Params {
  if !validBindingName(name) || names[name] { return fmt.Errorf("invalid or duplicate parameter name %q",name) } else { names[name]=true }
 }
 switch method.Name {
 case "class","yourself","=","~=","!=","signal","isNil","notNil","inspect","getVar:","setVar:to:","perform:","perform:with:","perform:with:with:","perform:with:with:with:","perform:withArguments:":return fmt.Errorf("%s is handled by the runtime rather than a native method body",method.Name)
 default:return nil
 }
}

func (obj *ObjectServer) cloneObject(patch PrototypePatch) Value {
 snapshot,err:=obj.nativeSnapshot()
 if err==nil { err=applyPrototypePatch(&snapshot,patch) } else {}
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 request:=SpawnRequest{ClassName:obj.classDef.Name,Snapshot:&snapshot}
 body,err:=json.Marshal(request)
 if err!=nil { panic(signalError{msg:err.Error()}) } else {}
 response,err:=rpcClient.Post(obj.coordinatorURL+"/spawn","application/json",bytes.NewReader(body))
 if err!=nil { panic(signalError{msg:"spawn clone: "+err.Error()}) } else {}
 defer response.Body.Close()
 var result SpawnResponse
 if err=json.NewDecoder(response.Body).Decode(&result);err!=nil { panic(signalError{msg:"decode cloned object: "+err.Error()}) } else if result.Error!="" { panic(signalError{msg:result.Error}) } else {}
 return ObjVal(result.ObjectID)
}

func (obj *ObjectServer) cloneDispatch(name string,args []Value) Value {
 if method,owner,found:=lookupMethod(obj.classDef,name);found {
  if obj.foreignBridge!=nil { return obj.dispatchForeign(name,args) } else { return obj.runMethod(method,owner,args) }
 } else {}
 patch:=PrototypePatch{}
 if name=="cloneWith:" {
  if len(args)!=1 || args[0].Kind!="string" { panic(signalError{msg:"cloneWith: expects a JSON object containing fields and methods"}) } else if err:=json.Unmarshal([]byte(args[0].Str),&patch);err!=nil { panic(signalError{msg:err.Error()}) } else {}
 } else {}
 return obj.cloneObject(patch)
}

func validateNativeSnapshot(snapshot *NativeSnapshot) error {
 if snapshot==nil || snapshot.Definition==nil { return fmt.Errorf("clone snapshot requires a native definition") } else {}
 depth:=0
 for definition:=snapshot.Definition;definition!=nil;definition=definition.Superclass {
  depth++
  if depth>64 || definition.Name=="" { return fmt.Errorf("invalid clone definition hierarchy") } else {}
  for name,method:=range definition.Methods {
   if method==nil || method.Name!=name { return fmt.Errorf("invalid clone method %q",name) } else if err:=validateSnapshotNodes(method.Body,0);err!=nil { return err } else {}
  }
 }
 if !cloneableClass(restoreDefinition(snapshot.Definition)) { return fmt.Errorf("class %s cannot use a native snapshot",snapshot.Definition.Name) } else {}
 if (snapshot.Definition.Name==BuiltinClassBlock)!=(snapshot.Block!=nil) { return fmt.Errorf("clone block state does not match its class") } else {}
 if snapshot.Block!=nil { if err:=validateSnapshotNodes(snapshot.Block.Body,0);err!=nil { return err } else {} } else {}
 values:=[]Value{}
 for _,value:=range snapshot.Fields { values=append(values,value) }
 for _,value:=range snapshot.Dictionary { values=append(values,value) }
 values=append(values,snapshot.Array...)
 return validateDebugValues(values)
}

func validateSnapshotNodes(nodes []*Node,depth int) error {
 if depth>256 { return fmt.Errorf("clone method syntax nesting exceeds 256") } else {}
 for _,node:=range nodes {
  if node==nil || node.Kind<NodeList || node.Kind>NodeString { return fmt.Errorf("clone contains an invalid syntax node") } else if err:=validateSnapshotNodes(node.Children,depth+1);err!=nil { return err } else {}
 }
 return nil
}

func (obj *ObjectServer) restoreNativeSnapshot(file string) error {
 body,err:=os.ReadFile(file)
 if err!=nil { return err } else {}
 defer os.Remove(file)
 var snapshot NativeSnapshot
 if err=json.Unmarshal(body,&snapshot);err!=nil { return err } else if err=validateNativeSnapshot(&snapshot);err!=nil { return err } else {}
 obj.classDef=restoreDefinition(snapshot.Definition)
 obj.instanceVars=copyDebugValues(snapshot.Fields)
 obj.arrayData=append([]Value{},snapshot.Array...)
 obj.dictionaryData=copyDebugValues(snapshot.Dictionary)
 obj.blockDef=snapshot.Block
 obj.prototypeID=snapshot.Prototype
 return nil
}

func nativeSnapshotArgument(snapshots []*NativeSnapshot) *NativeSnapshot {
 if len(snapshots)>0 { return snapshots[0] } else { return nil }
}

func snapshotArgumentFile(snapshot *NativeSnapshot) (string,error) {
 if err:=validateNativeSnapshot(snapshot);err!=nil { return "",err } else {}
 body,err:=json.Marshal(snapshot)
 if err!=nil { return "",err } else {}
 file,err:=os.CreateTemp("","smt-native-snapshot-*.json")
 if err!=nil { return "",err } else {}
 _,writeErr:=file.Write(body)
 closeErr:=file.Close()
 if writeErr!=nil { os.Remove(file.Name());return "",writeErr } else if closeErr!=nil { os.Remove(file.Name());return "",closeErr } else { return file.Name(),nil }
}
