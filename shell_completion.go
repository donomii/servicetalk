package main

import (
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strings"
 "time"
)

func (obj *ObjectServer) shellCompleteDispatch(args []Value) Value {
 if len(args)!=4 || args[0].Kind!="string" || args[1].Kind!="string" || args[2].Kind!="objid" || args[3].Kind!="string" { panic(signalError{msg:"Shell completion requires prefix, directory, environment and mode"}) } else {}
 environment:=obj.processEnvironment(args[2])
 if args[3].Str=="command" && !strings.ContainsAny(args[0].Str,"/\\") {
  return obj.shellStrings(shellCommandCandidates(args[0].Str,args[1].Str,environment))
 } else if args[3].Str=="path" || args[3].Str=="command" {
  return obj.shellStrings(shellPathCandidates(args[0].Str,args[1].Str,environment,args[3].Str=="command"))
 } else { panic(signalError{msg:"Unknown Shell completion mode: "+args[3].Str}) }
}

func shellEnvironmentValue(environment []string,name string) Value {
 for _,entry:=range environment {
  key,value,found:=strings.Cut(entry,"=")
  if found && processEnvironmentKey(key)==processEnvironmentKey(name) { return StrVal(value) } else {}
 }
 return Nil
}

func shellCommandCandidates(prefix,directory string,environment []string) []string {
 unique:=map[string]bool{}
 for _,path:=range filepath.SplitList(shellEnvironmentValue(environment,"PATH").Str) {
  if !filepath.IsAbs(path) { path=filepath.Join(directory,path) } else {}
  entries,err:=os.ReadDir(path)
  if err!=nil { continue } else {}
  for _,entry:=range entries {
   if entry.IsDir() || !strings.HasPrefix(entry.Name(),prefix) { continue } else {}
   if _,err:=exec.LookPath(filepath.Join(path,entry.Name()));err==nil { unique[entry.Name()]=true } else {}
  }
 }
 names:=make([]string,0,len(unique));for name:=range unique { names=append(names,name) };sort.Strings(names)
 return names
}

func shellPathCandidates(prefix,directory string,environment []string,executableOnly bool) []string {
 if prefix=="~" { return []string{"~"+string(filepath.Separator)} } else {}
 folder,base:=filepath.Split(prefix)
 resolved:=shellResolve(directory,folder,shellEnvironmentValue(environment,"HOME"),shellEnvironmentValue(environment,"USERPROFILE"))
 entries,err:=os.ReadDir(resolved)
 if os.IsNotExist(err) { return []string{} } else if err!=nil { shellFailure("complete directory",resolved,err) } else {}
 names:=[]string{}
 for _,entry:=range entries {
  if !strings.HasPrefix(entry.Name(),base) { continue } else {}
  suffix:=""
  info,err:=os.Stat(filepath.Join(resolved,entry.Name()))
  if err!=nil { continue } else if info.IsDir() { suffix=string(filepath.Separator) } else if executableOnly {
   if _,err:=exec.LookPath(filepath.Join(resolved,entry.Name()));err!=nil { continue } else {}
  } else {}
  names=append(names,folder+entry.Name()+suffix)
 }
 return names
}

func shellQuotedFragment(prefix string) (int,string,bool) {
 opening:=-1
 for i:=0;i<len(prefix);i++ {
  if prefix[i]!='\'' { continue } else if opening<0 { opening=i } else if i+1<len(prefix) && prefix[i+1]=='\'' { i++ } else { opening=-1 }
 }
 if opening<0 { return 0,"",false } else { return opening,strings.ReplaceAll(prefix[opening+1:],"''","'"),true }
}

func shellRemoteCompletions(shell ObjectEntry,method,prefix string) []string {
 reply,err:=sendNATSCall(shell.Subject,method,[]Value{StrVal(prefix)},3*time.Second)
 if err!=nil || reply.Signaled || reply.Result.Kind!="objid" { return nil } else {}
 subject:=natsSubjectForObject(reply.Result.ObjID)
 count,err:=sendNATSCall(subject,"size",nil,3*time.Second)
 if err!=nil || count.Signaled { return nil } else {}
 names:=[]string{}
 for i:=0;i<int(count.Result.Num);i++ {
  value,err:=sendNATSCall(subject,"at:",[]Value{NumVal(float64(i))},3*time.Second)
  if err!=nil || value.Signaled { return nil } else { names=append(names,value.Result.Str) }
 }
 return names
}

func shellQuotedCompletion(line string,position,opening int,fragment string,matches []string) (string,int,bool) {
 if len(matches)==0 { return "",0,false } else {}
 replacement:=matches[0]
 if len(matches)>1 { replacement=longestCommonPrefix(matches) } else {}
 if replacement==fragment && len(matches)>1 { return "",0,false } else {}
 replacement=strings.ReplaceAll(replacement,"'","''")
 if len(matches)==1 && !strings.HasSuffix(replacement,string(filepath.Separator)) { replacement+="' " } else {}
 completed:=line[:opening+1]+replacement+line[position:]
 return completed,opening+1+len(replacement),true
}
