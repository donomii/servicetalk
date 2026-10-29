package main

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "sync"
 "testing"
 "time"
)

func TestSpawnBundleOverridesHostAndEnvironmentSources(t *testing.T) {
 directory:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(directory,"BundledVersion.st"),[]byte("class BundledVersion [ value [ ^ 'host' ] ]"),0600);err!=nil { t.Fatal(err) } else {}
 t.Setenv("SMT_PATH",directory)
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),directory)
 for _,remote:=range []bool{false,true} {
  if remote { startBundleTestWorker(t,base,broker.ClientURL()) } else {}
  files:=[]SourceBundleFile{{Path:"BundledVersion.st",Data:[]byte("class BundledVersion [ value [ ^ 'prepared' ] ]")}}
  request:=SpawnRequest{ClassName:"BundledVersion",SourceBundle:&SourceBundle{Digest:digestSourceBundle(files),Files:files}}
  body,_:=json.Marshal(request);var response SpawnResponse
  if err:=json.Unmarshal(interactionTestPost(t,base,"/spawn",body),&response);err!=nil { t.Fatal(err) } else if response.Error!="" { t.Fatal(response.Error) } else {}
  result:=mcpTestCall(t,base,"call",MCPToolArguments{ObjectID:response.ObjectID,Method:"value"})
  if result.IsError || !strings.Contains(result.Content[0].Text,`"str":"prepared"`) { t.Fatalf("remote=%v: %+v",remote,result) } else {}
 }
}

func startBundleTestWorker(t *testing.T,base,broker string) {
 t.Helper();objects:=mcpTestCall(t,base,"objects",MCPToolArguments{});var entries []ObjectEntry
 if err:=json.Unmarshal([]byte(objects.Content[0].Text),&entries);err!=nil { t.Fatal(err) } else {}
 cluster:=strings.Split(entries[0].Subject,".")[1];bin,err:=os.Executable()
 if err!=nil { t.Fatal(err) } else {}
 port:=freePort(t);workerURL:="http://127.0.0.1:"+strconv.Itoa(port)
 command:=exec.Command(bin,"worker","--coordinator",base,"--host","127.0.0.1","--port",strconv.Itoa(port),"--verbose=false")
 command.Env=append(os.Environ(),"SMT_EMBED_NATS=",natsURLVar+"="+broker,natsClusterVar+"="+cluster)
 command.Stdout,command.Stderr=os.Stderr,os.Stderr
 if err:=command.Start();err!=nil { t.Fatal(err) } else {}
 done:=make(chan error,1);go func(){done<-command.Wait()}()
 t.Cleanup(func(){postShutdownAndWait(t,"bundle worker",workerURL,done)})
 waitForPing(t,workerURL+"/ping");waitForWorkerCount(t,base+"/metrics",1)
}

func TestBundlePreservesClassPathFormatPrecedence(t *testing.T) {
 first,second:=t.TempDir(),t.TempDir()
 for path,source:=range map[string]string{filepath.Join(first,"Version.st"):"class Version []",filepath.Join(second,"Version.smt"):"(class Version)",filepath.Join(second,"Other.st"):"class Other []"} {
  if err:=os.WriteFile(path,[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
 }
 original:=classPath;classPath=[]string{first,second};defer func(){classPath=original}()
 bundle,err:=classBundleFromClassPath()
 if err!=nil { t.Fatal(err) } else {}
 paths:=map[string]bool{};for _,file:=range bundle.Files { paths[file.Path]=true }
 if !paths["Version.st"] || paths["Version.smt"] || !paths["Other.st"] { t.Fatalf("bundle changed loader precedence: %v",paths) } else {}
}

func TestConcurrentBundleStageKeepsPublishedSourcesUnchanged(t *testing.T) {
 files:=[]SourceBundleFile{{Path:"Immutable.st",Data:[]byte("class Immutable [ version [ ^ "+strconv.FormatInt(time.Now().UnixNano(),10)+" ] ]")}}
 bundle:=&SourceBundle{Digest:digestSourceBundle(files),Files:files}
 directory,err:=stageSourceBundle(bundle)
 if err!=nil { t.Fatal(err) } else {}
 t.Cleanup(func(){os.RemoveAll(directory)})
 path:=filepath.Join(directory,"Immutable.st");stamp:=time.Unix(123456789,0)
 if err:=os.Chtimes(path,stamp,stamp);err!=nil { t.Fatal(err) } else {}
 var group sync.WaitGroup;failures:=make(chan error,12)
 for i:=0;i<12;i++ { group.Add(1);go func(){defer group.Done();_,err:=stageSourceBundle(bundle);failures<-err}() }
 group.Wait();close(failures)
 for err:=range failures { if err!=nil { t.Fatal(err) } else {} }
 info,err:=os.Stat(path)
 if err!=nil { t.Fatal(err) } else if !info.ModTime().Equal(stamp) { t.Fatal("staging rewrote an already-published source") } else {}
 if err:=os.WriteFile(path,[]byte("changed"),0600);err!=nil { t.Fatal(err) } else {}
 if _,err:=stageSourceBundle(bundle);err==nil { t.Fatal("modified immutable cache accepted") } else {}
}
