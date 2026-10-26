package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "testing"
)

func cloneTestInspect(t *testing.T,base,id string) InspectResponse {
 t.Helper()
 reply:=mcpTestCall(t,base,"inspect",MCPToolArguments{ObjectID:id})
 var result InspectResponse
 if reply.IsError { t.Fatal(reply.Content) } else if err:=json.Unmarshal([]byte(reply.Content[0].Text),&result);err!=nil { t.Fatal(err) } else {}
 return result
}

func TestCloneLoadedDefinitionAndInheritedSuper(t *testing.T) {
 dir:=t.TempDir()
 for name,source:=range map[string]string{"CloneParent.st":"class CloneParent [ value [ ^ 3 ] ]","CloneChild.st":"class CloneChild extends CloneParent [ | count | initialize [ count := 4 ] value [ ^ super value + count ] count: n [ count := n ] ]"} {
  if err:=os.WriteFile(filepath.Join(dir,name),[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
 }
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 sourceTestEval(t,base,"original := CloneChild new")
 if err:=os.WriteFile(filepath.Join(dir,"CloneParent.st"),[]byte("class CloneParent [ value [ ^ 100 ] ]"),0600);err!=nil { t.Fatal(err) } else {}
 if value:=sourceTestEval(t,base,"copy := original clone. copy value");value!="7" { t.Fatalf("clone loaded disk behavior: %s",value) } else {}
 if value:=sourceTestEval(t,base,"fresh := CloneChild new. fresh value");value!="104" { t.Fatalf("fresh definition: %s",value) } else {}
 if value:=sourceTestEval(t,base,"copy count: 10. original value");value!="7" { t.Fatalf("clone changed original state: %s",value) } else {}
 old:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"original"})
 copied:=testInteraction(t,base,InteractionRequest{Kind:"eval",Code:"copy"})
 before:=cloneTestInspect(t,base,old.Result.ObjID)
 after:=cloneTestInspect(t,base,copied.Result.ObjID)
 if before.Runtime.PID==after.Runtime.PID || before.Runtime.DefinitionVersion!=after.Runtime.DefinitionVersion || after.Runtime.Prototype!=old.Result.ObjID { t.Fatalf("clone identity/version: original=%+v clone=%+v",before.Runtime,after.Runtime) } else {}
}

func TestPrototypeDerivationAndCollectionClones(t *testing.T) {
 base:=interactiveTestRuntime(t)
 sourceTestEval(t,base,"original := Counter new. original add: 7")
 patch:=PrototypePatch{Fields:map[string]Value{"count":NumVal(20)},Methods:[]PrototypeMethod{{Name:"increment",Source:"count := count + 5"}}}
 body,_:=json.Marshal(patch)
 if value:=sourceTestEval(t,base,"derived := original cloneWith: "+smalltalkString(string(body))+". derived increment. derived value");value!="25" { t.Fatalf("derived behavior: %s",value) } else {}
 if value:=sourceTestEval(t,base,"grandchild := derived clone. grandchild increment. grandchild value");value!="30" { t.Fatalf("derived clone lost behavior: %s",value) } else {}
 if value:=sourceTestEval(t,base,"original value");value!="7" { t.Fatalf("derivation changed seed: %s",value) } else {}
 if value:=sourceTestEval(t,base,"a := {original. 2}. b := a clone. b at: 1 put: 9. a at: 1");value!="2" { t.Fatalf("array backing shared: %s",value) } else {}
 if value:=sourceTestEval(t,base,"(b at: 0) = original");value!="true" { t.Fatalf("shallow references changed: %s",value) } else {}
 if value:=sourceTestEval(t,base,"d := Dictionary new. d at: 'x' put: 4. e := d clone. e at: 'x' put: 9. d at: 'x'");value!="4" { t.Fatalf("dictionary backing shared: %s",value) } else {}
}

func TestCloneRejectsRuntimeResources(t *testing.T) {
 base:=interactiveTestRuntime(t)
 result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"Process new clone"})
 if !result.IsError { t.Fatalf("cloned process handle without a copy protocol: %+v",result) } else {}
}
