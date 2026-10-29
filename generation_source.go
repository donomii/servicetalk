package main

import (
 "fmt"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

// generationClassSource overrides embedded definitions only in an explicitly staged generation.
func generationClassSource(name string) (*ClassDef,bool,error) {
 dir:=os.Getenv("SMT_SOURCE_BUNDLE_DIR")
 if dir=="" { return nil,false,nil } else {}
 for _,suffix:=range []string{".smt",".st"} {
  path:=filepath.Join(dir,name+suffix)
  source,err:=os.ReadFile(path)
  if os.IsNotExist(err) { continue } else if err!=nil { return nil,true,fmt.Errorf("generation source %s: %w",path,err) } else {}
  var class *ClassDef
  if suffix==".st" { class,err=ParseSmalltalkClassSource(string(source)) } else { class,err=ParseClassSource(string(source)) }
  if err!=nil { return nil,true,fmt.Errorf("generation source %s: %w",path,err) } else { return class,true,nil }
 }
 path:=filepath.Join(dir,name)
 if info,err:=os.Stat(path);err==nil && info.IsDir() { class,err:=LoadForeignClass(path);return class,true,err } else { return nil,false,nil }
}

// generationSpawnRequest carries the immutable source version through constructors, blocks and clones.
func generationSpawnRequest(request SpawnRequest) SpawnRequest {
 dir:=os.Getenv("SMT_SOURCE_BUNDLE_DIR")
 if dir=="" { return request } else {}
 bundle,err:=inheritedSourceBundle(dir)
 if err!=nil { panic(signalError{msg:"read generation source for "+request.ClassName+": "+err.Error()}) } else {}
 request.SourceBundle=bundle
 return request
}

func serviceSourceBundle(source string) (string,*SourceBundle,error) {
 suffix:=".st"
 var class *ClassDef
 var err error
 if strings.HasPrefix(strings.TrimSpace(source),"(") { suffix=".smt";class,err=ParseClassSource(source) } else { class,err=ParseSmalltalkClassSource(source) }
 if err!=nil { return "",nil,fmt.Errorf("prepare service source: %w",err) } else {}
 if class.Name=="" || strings.ContainsAny(class.Name,"/\\.") { return "",nil,fmt.Errorf("prepare service source: invalid class name %q",class.Name) } else {}
 if err:=validateSourceDefinition(class.Name,class.Name+suffix,source);err!=nil { return "",nil,fmt.Errorf("prepare service source: %w",err) } else {}
 bundle,err:=classBundleFromClassPath()
 if err!=nil { return "",nil,err } else if bundle==nil { bundle=&SourceBundle{} } else {}
 files:=[]SourceBundleFile{}
 for _,file:=range bundle.Files {
  if file.Path==class.Name+".st" || file.Path==class.Name+".smt" || strings.HasPrefix(file.Path,class.Name+"/") { continue } else { files=append(files,file) }
 }
 files=append(files,SourceBundleFile{Path:class.Name+suffix,Data:[]byte(source)})
 sort.Slice(files,func(i,j int)bool{return files[i].Path<files[j].Path})
 if err:=validateSourceBundleSize(files);err!=nil { return "",nil,err } else { return class.Name,&SourceBundle{Digest:digestSourceBundle(files),Files:files},nil }
}
