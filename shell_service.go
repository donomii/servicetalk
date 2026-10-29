package main

import (
 "fmt"
 "io/fs"
 "os"
 "path/filepath"
 "runtime"
 "strings"
)

// shellDispatch implements the host filesystem boundary for the ordinary Shell class.
func (obj *ObjectServer) shellDispatch(args []Value) Value {
 if len(args)!=2 || args[0].Kind!="string" || args[1].Kind!="objid" { panic(signalError{msg:"Shell filesystem operation requires an operation name and argument Array"}) } else {}
 values:=obj.readArrayElements(args[1])
 operation:=args[0].Str
 want:=1
 switch operation { case "resolve": want=4;case "home","write","append","find","grep","environment": want=2 }
 if len(values)!=want { panic(signalError{msg:fmt.Sprintf("Shell %s requires %d arguments, received %d",operation,want,len(values))}) } else {}
 for i,value:=range values {
  optional:=operation=="home" || (operation=="resolve" && i>=2) || (operation=="environment" && i==1)
  if value.Kind!="string" && !(optional && value.Kind=="nil") { panic(signalError{msg:fmt.Sprintf("Shell %s argument %d must be a string",operation,i+1)}) } else {}
 }
 path:=values[0].Str
 switch operation {
 case "home": return StrVal(shellHome(values[0],values[1]))
 case "resolve": return StrVal(shellResolve(path,values[1].Str,values[2],values[3]))
 case "environment": shellEnvironmentName(path,values[1]);return Nil
 case "directory":
  info,err:=os.Stat(path)
  if err!=nil { shellFailure("change directory",path,err) } else if !info.IsDir() { shellFailure("change directory",path,fmt.Errorf("expected a directory")) } else {}
  return StrVal(path)
 case "list": return obj.shellList(path)
 case "read": return StrVal(shellRead(path))
 case "write","append": return StrVal(shellWrite(path,values[1].Str,operation=="append"))
 case "mkdir":
  if err:=os.MkdirAll(path,0755);err!=nil { shellFailure("create directory",path,err) } else {}
  return StrVal(path)
 case "find": return obj.shellFind(path,values[1].Str)
 case "grep":
  found:=[]string{}
  content:=shellRead(path)
  if content=="" { return obj.shellStrings(found) } else {}
  for _,line:=range strings.Split(strings.TrimSuffix(content,"\n"),"\n") {
   if strings.Contains(line,values[1].Str) { found=append(found,strings.TrimSuffix(line,"\r")) } else {}
  }
  return obj.shellStrings(found)
 default: panic(signalError{msg:"Unknown Shell filesystem operation: "+operation})
 }
}

func shellFailure(operation,path string,err error) {
 panic(signalError{msg:fmt.Sprintf("Cannot %s %q: %v",operation,path,err)})
}

func shellResolve(base,path string,homeValue,profile Value) string {
 if path=="~" || strings.HasPrefix(path,"~/") || strings.HasPrefix(path,"~\\") {
  path=filepath.Join(shellHome(homeValue,profile),strings.TrimLeft(path[1:],"/\\"))
 } else {}
 if !filepath.IsAbs(path) { path=filepath.Join(base,path) } else {}
 result,err:=filepath.Abs(path)
 if err!=nil { shellFailure("resolve path",path,err) } else { return result }
 return ""
}

func shellHome(home,profile Value) string {
 if home.Kind=="string" && home.Str!="" { return home.Str } else if runtime.GOOS=="windows" && profile.Kind=="string" && profile.Str!="" { return profile.Str } else { panic(signalError{msg:"Shell home directory is unset; set HOME (or USERPROFILE on Windows) with env:put:"}) }
}

func shellEnvironmentName(name string,value Value) {
 if name=="" || strings.ContainsAny(name,"=\x00") { panic(signalError{msg:"Environment variable name must be nonempty and contain neither '=' nor NUL"}) } else {}
 if value.Kind!="nil" && (value.Kind!="string" || strings.ContainsRune(value.Str,0)) { panic(signalError{msg:"Environment variable "+name+" requires a string without NUL, or nil to unset it"}) } else {}
}

func shellRead(path string) string {
 content,err:=os.ReadFile(path)
 if err!=nil { shellFailure("read file",path,err) } else { return string(content) }
 return ""
}

func shellWrite(path,text string,appendMode bool) string {
 flags:=os.O_WRONLY|os.O_CREATE|os.O_TRUNC
 if appendMode { flags=os.O_WRONLY|os.O_CREATE|os.O_APPEND } else {}
 file,err:=os.OpenFile(path,flags,0644)
 if err!=nil { shellFailure("open file for writing",path,err) } else {}
 _,writeErr:=file.WriteString(text)
 closeErr:=file.Close()
 if writeErr!=nil { shellFailure("write file",path,writeErr) } else if closeErr!=nil { shellFailure("close written file",path,closeErr) } else { return path }
 return ""
}

func (obj *ObjectServer) shellStrings(values []string) Value {
 id:=obj.spawnBuiltin(BuiltinClassArray)
 if id=="" { panic(signalError{msg:"Cannot create Array for Shell result"}) } else {}
 result:=ObjVal(id)
 for _,value:=range values { obj.sendMessage(result,"add:",[]Value{StrVal(value)}) }
 return result
}

func (obj *ObjectServer) shellList(path string) Value {
 entries,err:=os.ReadDir(path)
 if err!=nil { shellFailure("list directory",path,err) } else {}
 names:=make([]string,0,len(entries))
 for _,entry:=range entries { names=append(names,entry.Name()) }
 return obj.shellStrings(names)
}

func (obj *ObjectServer) shellFind(root,pattern string) Value {
 if _,err:=filepath.Match(pattern,"");err!=nil { shellFailure("match filename pattern",pattern,err) } else {}
 found:=[]string{}
 err:=filepath.WalkDir(root,func(path string,entry fs.DirEntry,walkErr error) error {
  if walkErr!=nil { return walkErr } else {}
  matched,err:=filepath.Match(pattern,entry.Name())
  if err!=nil { return err } else if !entry.Type().IsRegular() || !matched { return nil } else {}
  relative,err:=filepath.Rel(root,path)
  if err!=nil { return err } else { found=append(found,relative);return nil }
 })
 if err!=nil { shellFailure("find files",root,err) } else { return obj.shellStrings(found) }
 return Nil
}
