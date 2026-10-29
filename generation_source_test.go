package main

import (
 "os"
 "path/filepath"
 "sync"
 "testing"
 "time"
)

func TestGenerationBundleConcurrentFirstPublication(t *testing.T) {
 t.Setenv("XDG_CACHE_HOME",t.TempDir())
 files:=[]SourceBundleFile{{Path:"Fresh.st",Data:[]byte("class Fresh [ value [ ^ 42 ] ]")},{Path:"Helper.st",Data:[]byte("class Helper [ value [ ^ 7 ] ]")}}
 bundle:=&SourceBundle{Files:files,Digest:digestSourceBundle(files)}
 ready:=make(chan struct{});failures:=make(chan error,24);var group sync.WaitGroup
 for i:=0;i<24;i++ {
  group.Add(1)
  go func(){defer group.Done();<-ready;directory,err:=stageSourceBundle(bundle);if err==nil { _,err=os.ReadFile(filepath.Join(directory,"Helper.st")) } else {};failures<-err}()
 }
 close(ready);group.Wait();close(failures)
 for err:=range failures { if err!=nil { t.Fatal(err) } else {} }
}

func TestServiceSnapshotRejectsUnfinishedGeneration(t *testing.T) {
 object:=&ObjectServer{classDef:&ClassDef{Name:"Service",Methods:map[string]*MethodDef{}},instanceVars:map[string]Value{}}
 id,err:=object.serviceReserve("Example")
 if err!=nil { t.Fatal(err) } else {}
 if _,err:=object.nativeSnapshot();err==nil { t.Fatal("snapshot accepted preparing generation") } else {}
 if err:=object.servicePrepared(id,"Example","digest","target","");err!=nil { t.Fatal(err) } else {}
 if err:=object.serviceInitialized(id,false,"");err!=nil { t.Fatal(err) } else {}
 if _,err:=object.nativeSnapshot();err!=nil { t.Fatalf("idle candidate snapshot rejected: %v",err) } else {}
 generation,err:=object.serviceBegin(id,true)
 if err!=nil { t.Fatal(err) } else {}
 if _,err:=object.nativeSnapshot();err==nil { t.Fatal("snapshot accepted checking generation") } else {}
 if err:=object.serviceFinish(generation.ID,true,true,"reply lost");err!=nil { t.Fatal(err) } else {}
 if _,err:=object.nativeSnapshot();err==nil { t.Fatal("snapshot accepted uncertain generation") } else {}
}

func TestServiceSubclassOverridesKeepMailboxOrdering(t *testing.T) {
 base:=&ClassDef{Name:"Service",Methods:map[string]*MethodDef{"call:arguments:":{Name:"call:arguments:"}}}
 inherited:=&ObjectServer{classDef:&ClassDef{Name:"InheritedService",Superclass:base,Methods:map[string]*MethodDef{}}}
 overridden:=&ObjectServer{classDef:&ClassDef{Name:"CustomService",Superclass:base,Methods:map[string]*MethodDef{"call:arguments:":{Name:"call:arguments:"}}}}
 if !inherited.serviceConcurrentMethod("call:arguments:") { t.Fatal("inherited canonical wrapper should permit concurrent management") } else {}
 if overridden.serviceConcurrentMethod("call:arguments:") { t.Fatal("custom override bypassed ordinary mailbox") } else {}
}

type serviceTimeoutTransport struct {
 natsCoreTransport
 targetTimeout time.Duration
 unexpected bool
}

func (transport *serviceTimeoutTransport) Request(subject string,request MessageRequest,timeout time.Duration) (ReplyRequest,error) {
 result:=Nil
 switch request.Method {
 case "class":result=StrVal(BuiltinClassArray)
 case "size":if subject=="outer" { result=NumVal(3) } else { result=NumVal(0) }
 case "at:":result=[]Value{StrVal("version"),ObjVal("inner"),NumVal(120000)}[int(request.Args[0].Num)]
 case "version":transport.targetTimeout=timeout;result=NumVal(42)
 default:transport.unexpected=true
 }
 return ReplyRequest{Result:result},nil
}

func TestServiceNativeBoundaryPreservesLongCallDeadline(t *testing.T) {
 transport:=&serviceTimeoutTransport{};previous:=activeTransport;activeTransport=transport;defer func(){activeTransport=previous}()
 object:=&ObjectServer{id:"service",classDef:&ClassDef{Name:"Service"},instanceVars:map[string]Value{"serviceState":StrVal(`{"current":"g1","generations":[{"id":"g1","target":"target","state":"active"}]}`),serviceTargetKey("g1"):ObjVal("target")}}
 for _,id:=range []string{"outer","inner","target"} { object.registry.Store(id,ObjectEntry{ID:id,Subject:id}) }
 result:=object.sendMessage(ObjVal("service"),"__service:arguments:",[]Value{StrVal("call"),ObjVal("outer")})
 if result!=NumVal(42) || transport.unexpected || transport.targetTimeout!=120*time.Second { t.Fatalf("nested boundary lost deadline: result=%+v timeout=%s unexpected=%v",result,transport.targetTimeout,transport.unexpected) } else {}
 if deadline:=messageCallTimeout("call:arguments:timeoutMs:",[]Value{StrVal("version"),ObjVal("arguments"),NumVal(120000)});deadline!=125*time.Second { t.Fatalf("outer deadline %s",deadline) } else {}
}
