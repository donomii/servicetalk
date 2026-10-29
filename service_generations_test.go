package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
 "testing/fstest"
 "time"
)

func serviceTestWrite(t *testing.T,dir,name,source string) {
 t.Helper()
 if err:=os.MkdirAll(filepath.Dir(filepath.Join(dir,name)),0700);err!=nil { t.Fatal(err) } else {}
 if err:=os.WriteFile(filepath.Join(dir,name),[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
}

func serviceTestStatus(t *testing.T,base string) ServiceStatus {
 t.Helper()
 var status ServiceStatus
 if err:=json.Unmarshal([]byte(sourceTestEval(t,base,"service status")),&status);err!=nil { t.Fatal(err) } else { return status }
 return status
}

func serviceTestSetup(t *testing.T,dir string) string {
 t.Helper()
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 sourceTestEval(t,base,"service := Service new. service name: 'evaluation'")
 return base
}

func serviceTestPromote(t *testing.T,base,prepare,expected string) {
 t.Helper()
 sourceTestEval(t,base,"generation := service "+prepare+". service check: generation selector: 'version' arguments: {} expect: "+smalltalkString(expected)+". service promote: generation")
}

func TestServiceRollingGenerationDrainsAndRetainsDirectReferences(t *testing.T) {
 entered:=make(chan struct{},1);release:=make(chan struct{})
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){entered<-struct{}{};<-release;fmt.Fprint(w,"complete")}))
 defer server.Close();defer close(release)
 dir:=t.TempDir()
 v1:="class Rolling [ version [ ^ 'v1' ] slow: url [ HttpClient new get: url. ^ 'v1' ] helper [ ^ GenerationHelper new version ] closure [ ^ [GenerationHelper new version] ] ]"
 serviceTestWrite(t,dir,"Rolling.st",v1)
 serviceTestWrite(t,dir,"GenerationHelper.st","class GenerationHelper [ version [ ^ 'helper-v1' ] ]")
 base:=serviceTestSetup(t,dir)
 serviceTestPromote(t,base,"prepareClass: 'Rolling'","v1")
 sourceTestEval(t,base,"old := service current. oldGeneration := generation. oldClosure := old closure. oldClone := old clone. pending := Promise new. pending send: service selector: 'call:arguments:' arguments: {'slow:'. {"+smalltalkString(server.URL)+"}}")
 select { case <-entered:case <-time.After(5*time.Second):t.Fatal("v1 never reached its outstanding call") }
 serviceTestWrite(t,dir,"GenerationHelper.st","class GenerationHelper [ version [ ^ 'helper-v2' ] ]")
 v2:="class Rolling [ version [ ^ 'v2' ] helper [ ^ GenerationHelper new version ] ]"
 serviceTestPromote(t,base,"prepareSource: "+smalltalkString(v2),"v2")
 if got:=sourceTestEval(t,base,"service call: 'version' arguments: {}");got!="v2" { t.Fatalf("new traffic reached %s",got) } else {}
 state:=serviceTestStatus(t,base)
 if state.Generations[0].State!="draining" || state.Generations[0].InFlight!=1 || state.Current!="g2" { t.Fatalf("v1 not draining alongside v2: %+v",state) } else {}
 release<-struct{}{}
 if got:=sourceTestEval(t,base,"pending await");got!="v1" { t.Fatalf("outstanding v1 changed result: %s",got) } else {}
 if got:=sourceTestEval(t,base,"{old version. old helper. oldClone helper. oldClosure value. service call: 'helper' arguments: {}} join: ','");got!="v1,helper-v1,helper-v1,helper-v1,helper-v2" { t.Fatalf("generation source lineage changed: %s",got) } else {}
 state=serviceTestStatus(t,base)
 if state.Generations[0].State!="retired" || state.Generations[0].InFlight!=0 || state.Generations[0].Version==state.Generations[1].Version { t.Fatalf("retirement/version failure: %+v",state) } else {}
 if got:=sourceTestEval(t,base,"service targetFor: oldGeneration");got!="nil" { t.Fatalf("service retained drained target: %s",got) } else {}
}

func TestServiceFailedCandidatesDoNotReplaceActiveGeneration(t *testing.T) {
 base:=serviceTestSetup(t,t.TempDir())
 serviceTestPromote(t,base,"prepareSource: 'class Healthy [ version [ ^ ''good'' ] ]'","good")
 for _,code:=range []string{
  "service prepareSource: 'class Broken ['",
  "service prepareSource: 'class BadInit [ initialize [ ''initialization failed'' signal ] ]'",
  "bad := service prepareSource: 'class BadCheck [ version [ ^ ''wrong'' ] ]'. service check: bad selector: 'version' arguments: {} expect: 'good'",
  "service promote: bad",
 } {
  result:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:code})
  if !result.IsError { t.Fatalf("invalid candidate succeeded: %s",code) } else {}
  if got:=sourceTestEval(t,base,"service call: 'version' arguments: {}");got!="good" { t.Fatalf("failed candidate changed active: %s",got) } else {}
 }
 state:=serviceTestStatus(t,base)
 if state.Current!="g1" || len(state.Generations)!=4 { t.Fatalf("failed candidates not recorded: %+v",state) } else {}
 for _,candidate:=range state.Generations[1:] { if candidate.State!="failed" || candidate.Error=="" { t.Fatalf("candidate failure missing: %+v",candidate) } else {} }
 sourceTestEval(t,base,"service discard: bad")
 if got:=sourceTestEval(t,base,"service targetFor: bad");got!="nil" { t.Fatalf("discard retained candidate: %s",got) } else {}
}

func TestServiceTimeoutRetainsUnknownCallUntilForced(t *testing.T) {
 entered:=make(chan struct{},1);release:=make(chan struct{})
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){entered<-struct{}{};<-release;fmt.Fprint(w,"done")}))
 defer server.Close();defer close(release)
 base:=serviceTestSetup(t,t.TempDir())
 serviceTestPromote(t,base,"prepareSource: 'class Waiting [ version [ ^ ''old'' ] slow: url [ HttpClient new get: url. ^ 7 ] ]'","old")
 sourceTestEval(t,base,"old := service current. oldGeneration := generation")
 failure:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"service call: 'slow:' arguments: {"+smalltalkString(server.URL)+"} timeoutMs: 100"})
 if !failure.IsError || !strings.Contains(failure.Content[0].Text,"not retried") { t.Fatalf("timeout was hidden: %+v",failure) } else {}
 select { case <-entered:case <-time.After(time.Second):t.Fatal("timed call never began") }
 serviceTestPromote(t,base,"prepareSource: 'class Waiting [ version [ ^ ''new'' ] ]'","new")
 state:=serviceTestStatus(t,base)
 if state.Generations[0].State!="draining" || state.Generations[0].Uncertain!=1 { t.Fatalf("timed call silently drained: %+v",state) } else {}
 sourceTestEval(t,base,"service forceRetire: oldGeneration")
 state=serviceTestStatus(t,base)
 if state.Generations[0].State!="forced" || state.Generations[0].Uncertain!=0 { t.Fatalf("force not recorded: %+v",state) } else {}
 if got:=sourceTestEval(t,base,"service call: 'version' arguments: {}");got!="new" { t.Fatalf("force stopped current generation: %s",got) } else {}
}

func TestServiceForeignRebuildKeepsOldBinaryAndFailedBuild(t *testing.T) {
 if _,err:=exec.LookPath("cc");err!=nil { t.Skip("C compiler required") } else {}
 dir:=t.TempDir()
 serviceTestWrite(t,dir,"NativeVersion/NativeVersion.smt","(foreign-class NativeVersion (cmethod initialize initialize) (cmethod version version))")
 source:="#include \"_st.h\"\nValue initialize(int n,Value* a){return st_nil();}\nValue version(int n,Value* a){return st_string(\"%s\",2);}\n"
 serviceTestWrite(t,dir,"NativeVersion/version.c",fmt.Sprintf(source,"v1"))
 base:=serviceTestSetup(t,dir)
 serviceTestPromote(t,base,"prepareClass: 'NativeVersion'","v1")
 sourceTestEval(t,base,"old := service current")
 serviceTestWrite(t,dir,"NativeVersion/version.c",fmt.Sprintf(source,"v2"))
 serviceTestPromote(t,base,"prepareClass: 'NativeVersion'","v2")
 if got:=sourceTestEval(t,base,"{old version. service call: 'version' arguments: {}} join: ','");got!="v1,v2" { t.Fatalf("foreign binary generations mixed: %s",got) } else {}
 serviceTestWrite(t,dir,"NativeVersion/version.c","this is not C")
 failure:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"service prepareClass: 'NativeVersion'"})
 if !failure.IsError { t.Fatalf("broken C candidate built: %+v",failure) } else {}
 if got:=sourceTestEval(t,base,"service call: 'version' arguments: {}");got!="v2" { t.Fatalf("failed foreign build replaced v2: %s",got) } else {}
}

func TestServiceStagedSourceOverridesEmbeddedOnlyForGeneration(t *testing.T) {
 previous:=embeddedClasses;embeddedClasses=fstest.MapFS{"Fixed.st":{Data:[]byte("class Fixed [ version [ ^ 1 ] ]")}}
 defer func(){embeddedClasses=previous}()
 dir:=t.TempDir();serviceTestWrite(t,dir,"Fixed.st","class Fixed [ version [ ^ 2 ] ]")
 t.Setenv("SMT_SOURCE_BUNDLE_DIR",dir)
 class,err:=LoadClass("Fixed")
 if err!=nil || !strings.Contains(class.Methods["version"].Body[0].String(),"2") { t.Fatalf("generation did not override embedded: %+v, %v",class,err) } else {}
 t.Setenv("SMT_SOURCE_BUNDLE_DIR","")
 class,err=LoadClass("Fixed")
 if err!=nil || !strings.Contains(class.Methods["version"].Body[0].String(),"1") { t.Fatalf("normal embedded priority changed: %+v, %v",class,err) } else {}
}

func TestServiceForcedForeignCallReportsInterruption(t *testing.T) {
 if runtime.GOOS=="windows" { t.Skip("fixture uses POSIX sleep") } else if _,err:=exec.LookPath("cc");err!=nil { t.Skip("C compiler required") } else {}
 dir:=t.TempDir();marker:=filepath.Join(dir,"entered")
 serviceTestWrite(t,dir,"Busy/Busy.smt","(foreign-class Busy (cmethod initialize initialize) (cmethod version version) (cmethod wait: wait_call))")
 serviceTestWrite(t,dir,"Busy/busy.c",`#include "_st.h"
#include <stdio.h>
#include <unistd.h>
Value initialize(int n,Value* a){return st_nil();}
Value version(int n,Value* a){return st_cstr("old");}
Value wait_call(int n,Value* a){FILE* f=fopen(st_bytes(a[0],NULL),"w");if(f){fputs("started",f);fclose(f);}else{st_signal("cannot create marker");}sleep(5);return st_cstr("unexpected completion");}
`)
 base:=serviceTestSetup(t,dir)
 serviceTestPromote(t,base,"prepareClass: 'Busy'","old")
 sourceTestEval(t,base,"oldGeneration := generation. pending := Promise new. pending send: service selector: 'call:arguments:' arguments: {'wait:'. {"+smalltalkString(marker)+"}}")
 deadline:=time.Now().Add(3*time.Second)
 for {
  if _,err:=os.Stat(marker);err==nil { break } else if time.Now().After(deadline) { t.Fatal("foreign call never started") } else { time.Sleep(10*time.Millisecond) }
 }
 serviceTestPromote(t,base,"prepareSource: 'class Replacement [ version [ ^ ''new'' ] ]'","new")
 started:=time.Now()
 sourceTestEval(t,base,"service forceRetire: oldGeneration")
 failure:=mcpTestCall(t,base,"eval",MCPToolArguments{Code:"pending await"})
 if !failure.IsError || !strings.Contains(failure.Content[0].Text,"forced retirement") { t.Fatalf("forced call reported success: %+v",failure) } else if time.Since(started)>2*time.Second { t.Fatal("force waited for ordinary foreign method to finish") } else {}
}
