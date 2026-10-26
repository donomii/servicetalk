package main

import (
 "bytes"
 "encoding/json"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestObjectWorkspaceIsolationIdentityAndClosures(t *testing.T) {
 dir := t.TempDir()
 for _, name := range []string{"Thermometer.st", "ObjectWorkspace.smt"} {
  data, err := os.ReadFile(name)
  if err != nil { t.Fatal(err) } else {}
  if err := os.WriteFile(filepath.Join(dir,name),data,0600); err != nil { t.Fatal(err) } else {}
 }
 broker := startNATSTestServerOnPort(t,-1); t.Cleanup(broker.Shutdown)
 base := startMCPTestCoordinator(t,broker.ClientURL(),dir)
 created := mcpTestCall(t,base,"eval",MCPToolArguments{Code:"a := Thermometer new. b := Thermometer new"})
 if created.IsError { t.Fatalf("create: %+v",created) } else {}
 listing := mcpTestCall(t,base,"objects",MCPToolArguments{})
 var entries []ObjectEntry
 if err := json.Unmarshal([]byte(listing.Content[0].Text),&entries); err != nil { t.Fatal(err) } else {}
 ids := []string{}
 for _, entry := range entries { if entry.ClassName=="Thermometer" { ids=append(ids,entry.ID) } else {} }
 if len(ids)!=2 { t.Fatalf("expected two targets, got %v",ids) } else {}
 evaluate := func(id, code string) WorkspaceResult {
  t.Helper()
  body, err := json.Marshal(ObjectWorkspaceRequest{ObjectID:id,Code:code})
  if err != nil { t.Fatal(err) } else {}
  response, err := http.Post(base+"/workspace","application/json",bytes.NewReader(body))
  if err != nil { t.Fatal(err) } else {}
  defer response.Body.Close()
  var result WorkspaceResult
  if err := json.NewDecoder(response.Body).Decode(&result); err != nil { t.Fatal(err) } else {}
  return result
 }
 for _, test := range []struct{ id, code string; want Value }{
  {ids[0],"saved := 7. self celsius: 30. self celsius",NumVal(30)},
  {ids[0],"saved + it",NumVal(37)},
  {ids[1],"self celsius",NumVal(20)},
  {ids[1],"saved",Nil},
  {ids[0],"closure := [ self celsius ]. closure value",NumVal(30)},
  {ids[0],"self celsius: 31. closure value",NumVal(31)},
  {ids[0],"nested := [ [ self celsius ] ]. (nested value) value",NumVal(31)},
  {ids[0],"self",ObjVal(ids[0])},
  {ids[0],"it celsius",NumVal(31)},
 } {
  result := evaluate(test.id,test.code)
  if result.Error!="" || result.Result!=test.want { t.Fatalf("%s: got %+v, want %+v",test.code,result,test.want) } else {}
 }
 failure := evaluate(ids[0],"self celsius: -300")
 if !strings.Contains(failure.Error,"absolute zero") { t.Fatalf("expected setter failure, got %+v",failure) } else {}
 if result := evaluate(ids[0],"it"); result.Result!=NumVal(31) { t.Fatalf("failed evaluation changed it: %+v",result) } else {}
 if result := evaluate(ids[0],"Transcript show: 'hello'. self"); result.Output!="hello" || result.Result!=ObjVal(ids[0]) { t.Fatalf("typed result and transcript: %+v",result) } else {}
}

func TestBoundBlockReferencesReceiver(t *testing.T) {
 receiver := ObjVal("target")
 object := &ObjectServer{blockDef:&BlockDef{ParentID:"workspace",SelfOverride:&receiver}}
 refs := object.collectObjidRefs()
 found := false
 for _, ref := range refs { if ref=="target" { found=true } else {} }
 if !found { t.Fatalf("bound receiver missing from references: %v",refs) } else {}
}
