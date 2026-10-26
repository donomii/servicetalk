package main

import (
 "crypto/sha256"
 "encoding/json"
 "fmt"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "sync"
)

type sourceStore struct { mu sync.Mutex }

func sourceDigest(source string) string { return fmt.Sprintf("%x",sha256.Sum256([]byte(source))) }

func sourceDefinition(class string) (SourceResponse,error) {
 known:=false
 for _,name:=range navigationClassNames() { if name==class { known=true;break } else {} }
 if !known { return SourceResponse{},fmt.Errorf("class %q is not in the source browser",class) } else {}
 source,file:=navigationSource(class)
 if file=="" { return SourceResponse{},fmt.Errorf("class %q has no browsable definition",class) } else {}
 result:=SourceResponse{Class:class,File:file,Source:source,Digest:sourceDigest(source),Revisions:[]SourceRevision{}}
 if strings.HasPrefix(file,"embedded:") { result.ReadOnly=true;return result,nil } else {}
 absolute,err:=filepath.Abs(file)
 if err==nil { absolute,err=filepath.EvalSymlinks(absolute) } else {}
 if err!=nil { return result,err } else { result.File=absolute }
 definition,err:=loadClassRaw(class)
 if err!=nil { return result,err } else { result.ReadOnly=definition.Foreign!=nil }
 return result,nil
}

func validateSourceDefinition(class,file,source string) error {
 var nodes []*Node
 var err error
 if filepath.Ext(file)==".st" { nodes,err=ParseSmalltalk(source) } else { nodes,err=Parse(source) }
 if err!=nil { return err } else if len(nodes)!=1 { return fmt.Errorf("definition must contain exactly one class") } else {}
 node:=nodes[0]
 if node.Kind!=NodeList || len(node.Children)<2 || node.Children[0].Str!="class" { return fmt.Errorf("definition must contain a class") } else {}
 definition,err:=parseClassNode(node)
 if err!=nil { return err } else if definition.Name!=class { return fmt.Errorf("definition declares %q; expected %q",definition.Name,class) } else {}
 seen:=map[string]bool{class:true}
 for parent:=definition.SuperclassName;parent!=""; {
  if seen[parent] { return fmt.Errorf("cyclic superclass %q",parent) } else { seen[parent]=true }
  loaded,err:=loadClassRaw(parent)
  if err!=nil { return err } else { parent=loaded.SuperclassName }
 }
 return nil
}

func (store *sourceStore) save(file,expected,source string) error {
 store.mu.Lock()
 defer store.mu.Unlock()
 previous,err:=os.ReadFile(file)
 if err!=nil { return err } else if sourceDigest(string(previous))!=expected { return fmt.Errorf("definition changed since it was opened; reload it before saving") } else {}
 info,err:=os.Stat(file)
 if err!=nil { return err } else {}
 temporary,err:=os.CreateTemp(filepath.Dir(file),".servicetalk-source-*")
 if err!=nil { return err } else {}
 defer os.Remove(temporary.Name())
 if err=temporary.Chmod(info.Mode().Perm());err==nil { _,err=temporary.WriteString(source) } else {}
 if err==nil { err=temporary.Sync() } else {}
 closeErr:=temporary.Close()
 if err!=nil { return err } else if closeErr!=nil { return closeErr } else {}
 return os.Rename(temporary.Name(),file)
}

func (c *Coordinator) sourceGit(file,method string,args []Value) (Value,string,error) {
 directory:=filepath.Dir(file)
 git,err:=c.ensureUIObject("sourceGit:"+directory,"Git")
 if err!=nil { return Nil,"",err } else {}
 if _,err=c.callObjectID(git.ObjID,"directory:",[]Value{StrVal(directory)},evalTimeout());err!=nil { return Nil,"",err } else {}
 value,err:=c.callObjectID(git.ObjID,method,args,evalTimeout())
 return value,git.ObjID,err
}

func (c *Coordinator) readSourceHistory(result *SourceResponse) {
 if strings.HasPrefix(result.File,"embedded:") { return } else {}
 value,gitID,err:=c.sourceGit(result.File,"history:",[]Value{StrVal(filepath.Base(result.File))})
 result.GitObject=gitID
 if err!=nil { result.GitError=err.Error();return } else {}
 for _,line:=range strings.Split(strings.TrimSpace(value.Str),"\n") {
  parts:=strings.SplitN(line,"\x00",3)
  if len(parts)==3 { result.Revisions=append(result.Revisions,SourceRevision{ID:parts[0],Timestamp:parts[1],Subject:parts[2]}) } else {}
 }
}

func validSourceRevision(revision string) bool {
 if len(revision)!=40 && len(revision)!=64 { return false } else {}
 for _,ch:=range revision { if !strings.ContainsRune("0123456789abcdef",ch) { return false } else {} }
 return true
}

func (c *Coordinator) sourceRequest(request SourceRequest) SourceResponse {
 result,err:=sourceDefinition(request.Class)
 if err!=nil { result.Error=err.Error();return result } else {}
 switch request.Action {
 case "read":c.readSourceHistory(&result)
 case "save":
  if result.ReadOnly { err=fmt.Errorf("this definition is read-only in the source editor") } else if err=validateSourceDefinition(request.Class,result.File,request.Source);err==nil { err=c.sources.save(result.File,request.Digest,request.Source) } else {}
  if err==nil { result.Source=request.Source;result.Digest=sourceDigest(request.Source);c.readSourceHistory(&result) } else {}
 case "revision":
  if !validSourceRevision(request.Revision) { err=fmt.Errorf("invalid Git revision") } else {
   var value Value
   value,result.GitObject,err=c.sourceGit(result.File,"version:file:",[]Value{StrVal(request.Revision),StrVal("./"+filepath.Base(result.File))})
   if err==nil { result.HistoricalSource=value.Str } else {}
  }
 case "diff":
  var value Value
  value,result.GitObject,err=c.sourceGit(result.File,"diff:",[]Value{StrVal(filepath.Base(result.File))})
  result.Diff=value.Str
 case "commit":
  if strings.TrimSpace(request.Message)=="" { err=fmt.Errorf("enter a commit message") } else if request.Digest!=result.Digest { err=fmt.Errorf("definition changed since the displayed version; reload before committing") } else {
   _,result.GitObject,err=c.sourceGit(result.File,"commitFile:message:",[]Value{StrVal(filepath.Base(result.File)),StrVal(request.Message)})
   if err==nil { c.readSourceHistory(&result) } else {}
  }
 default:err=fmt.Errorf("unknown source action %q",request.Action)
 }
 if err!=nil { result.Error=err.Error() } else {}
 return result
}

func (c *Coordinator) handleSource(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"source requires POST",http.StatusMethodNotAllowed);return } else {}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request SourceRequest
 if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { json.NewEncoder(w).Encode(SourceResponse{Error:err.Error()}) } else { json.NewEncoder(w).Encode(c.sourceRequest(request)) }
}
