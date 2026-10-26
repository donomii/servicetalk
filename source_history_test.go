package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func sourceTestRequest(t *testing.T,base string,request SourceRequest) SourceResponse {
 t.Helper()
 body,_:=json.Marshal(request)
 var result SourceResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/source",body),&result);err!=nil { t.Fatal(err) } else {}
 return result
}

func sourceTestEval(t *testing.T,base,code string) string {
 t.Helper()
 result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
 if result.IsError { t.Fatalf("%s: %+v",code,result) } else {}
 return result.Content[0].Text
}

func TestSourceHistoryGitAndCoexistingDefinitions(t *testing.T) {
 dir:=t.TempDir()
 original:="class Versioned [ initialize [ nil ] value [ ^ 1 ] ]"
 changed:="class Versioned [ initialize [ nil ] value [ ^ 2 ] ]"
 for name,content:=range map[string]string{"Versioned.st":original,"Unrelated.txt":"before"} {
  if err:=os.WriteFile(filepath.Join(dir,name),[]byte(content),0600);err!=nil { t.Fatal(err) } else {}
 }
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 sourceTestEval(t,base,"testGit := Git new. testGit directory: "+smalltalkString(dir)+". testGit checked: #('init'). testGit checked: #('config' 'user.name' 'ServiceTalk Test'). testGit checked: #('config' 'user.email' 'test@example.invalid'). testGit add: #('Versioned.st' 'Unrelated.txt'). testGit commit: 'Initial version'")
 read:=sourceTestRequest(t,base,SourceRequest{Action:"read",Class:"Versioned"})
 if read.Error!="" || read.GitError!="" || len(read.Revisions)!=1 || read.Source!=original || read.GitObject=="" { t.Fatalf("read: %+v",read) } else {}
 oldID:=sourceTestEval(t,base,"oldVersion := Versioned new. oldVersion value")
 if oldID!="1" { t.Fatalf("old instance: %q",oldID) } else {}
 saved:=sourceTestRequest(t,base,SourceRequest{Action:"save",Class:"Versioned",Digest:read.Digest,Source:changed})
 if saved.Error!="" || saved.Source!=changed || saved.Digest==read.Digest { t.Fatalf("save: %+v",saved) } else {}
 if value:=sourceTestEval(t,base,"newVersion := Versioned new. newVersion value");value!="2" { t.Fatalf("new instance: %q",value) } else {}
 if value:=sourceTestEval(t,base,"oldVersion value");value!="1" { t.Fatalf("changed old instance: %q",value) } else {}
 stale:=sourceTestRequest(t,base,SourceRequest{Action:"save",Class:"Versioned",Digest:read.Digest,Source:original})
 if !strings.Contains(stale.Error,"changed since") { t.Fatalf("stale save: %+v",stale) } else {}
 invalid:=sourceTestRequest(t,base,SourceRequest{Action:"save",Class:"Versioned",Digest:saved.Digest,Source:"class Wrong [ value [ ^ 3 ] ]"})
 if !strings.Contains(invalid.Error,"expected") { t.Fatalf("invalid definition: %+v",invalid) } else {}
 if err:=os.WriteFile(filepath.Join(dir,"Unrelated.txt"),[]byte("after"),0600);err!=nil { t.Fatal(err) } else {}
 sourceTestEval(t,base,"testGit add: #('Unrelated.txt')")
 committed:=sourceTestRequest(t,base,SourceRequest{Action:"commit",Class:"Versioned",Digest:saved.Digest,Message:"New instances return two"})
 if committed.Error!="" || len(committed.Revisions)!=2 { t.Fatalf("commit: %+v",committed) } else {}
 if staged:=sourceTestEval(t,base,"testGit checked: #('diff' '--cached' '--name-only')");strings.TrimSpace(staged)!="Unrelated.txt" { t.Fatalf("commit disturbed other staged work: %q",staged) } else {}
 revision:=sourceTestRequest(t,base,SourceRequest{Action:"revision",Class:"Versioned",Revision:read.Revisions[0].ID})
 if revision.Error!="" || revision.HistoricalSource!=original { t.Fatalf("revision: %+v",revision) } else {}
 outside:=sourceTestRequest(t,base,SourceRequest{Action:"read",Class:"../Versioned"})
 if outside.Error=="" { t.Fatal("accepted class outside browser") } else {}
}

func TestDefinitionValidationRejectsCyclesAndExtraCode(t *testing.T) {
 for _,source:=range []string{"class Trial extends Trial [ value [ ^ 1 ] ]","class Trial [ value [ ^ 1 ] ]. 42","class Trial [ value ["} {
  if err:=validateSourceDefinition("Trial","Trial.st",source);err==nil { t.Fatalf("accepted invalid source: %s",source) } else {}
 }
}
