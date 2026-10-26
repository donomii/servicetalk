package main

import (
 "encoding/json"
 "fmt"
 "net/http"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
)

type selectorReplacement struct { span SelectorSpan; text string }

func selectorParts(selector string) (string,[]string,error) {
 if validBindingName(selector) { return "unary",[]string{selector},nil } else {}
 if strings.HasSuffix(selector,":") {
  parts:=strings.Split(strings.TrimSuffix(selector,":"),":")
  for index,part:=range parts { if !validBindingName(part) { return "",nil,fmt.Errorf("invalid selector %q",selector) } else { parts[index]=part+":" } }
  return "keyword",parts,nil
 } else {}
 if selector!="" { for _,ch:=range selector { if !isStBinopChar(ch) { return "",nil,fmt.Errorf("invalid selector %q",selector) } else {} };return "binary",[]string{selector},nil } else { return "",nil,fmt.Errorf("enter a selector") }
}

func refactorSelector(source,file,from,to string) (string,[]RefactorEdit,[]int,error) {
 oldKind,oldParts,err:=selectorParts(from)
 if err!=nil { return "",nil,nil,err } else {}
 newKind,newParts,err:=selectorParts(to)
 if err!=nil { return "",nil,nil,err } else if oldKind!=newKind || len(oldParts)!=len(newParts) || from==to { return "",nil,nil,fmt.Errorf("choose a different selector with the same syntax and argument count") } else {}
 sites,literals,err:=selectorLocations(source,file,from)
 if err!=nil { return "",nil,nil,err } else {}
 hasOld,hasNew:=false,false
 for _,site:=range sites { if site.Kind=="definition" && site.Selector==from { hasOld=true } else if site.Kind=="definition" && site.Selector==to { hasNew=true } else {} }
 if hasOld && hasNew { return "",nil,nil,fmt.Errorf("definition already contains %s",to) } else {}
 replacements:=[]selectorReplacement{};edits:=[]RefactorEdit{};runes:=[]rune(source)
 for _,site:=range sites {
  if site.Selector!=from { continue } else {}
  for index,span:=range site.Spans {
   replacement:=to
   if filepath.Ext(file)==".st" { replacement=newParts[index] } else if span.Start<len(runes) && runes[span.Start]=='"' { replacement=strconv.Quote(to) } else {}
   replacements=append(replacements,selectorReplacement{span:span,text:replacement})
  }
  edits=append(edits,RefactorEdit{Line:site.Spans[0].Line,Kind:site.Kind,Before:from,After:to})
 }
 sort.Slice(replacements,func(i,j int)bool{return replacements[i].span.Start>replacements[j].span.Start})
 for _,replacement:=range replacements { span:=replacement.span;if span.Start<0 || span.End>len(runes) || span.End<=span.Start { return "",nil,nil,fmt.Errorf("invalid parsed selector location") } else {};runes=append(append(append([]rune{},runes[:span.Start]...),[]rune(replacement.text)...),runes[span.End:]...) }
 sort.Slice(edits,func(i,j int)bool{return edits[i].Line<edits[j].Line})
 return string(runes),edits,literals,nil
}

func refactorPreview(request RefactorRequest) RefactorResponse {
 response:=RefactorResponse{Files:[]RefactorFile{},Errors:[]string{}}
 if _,_,_,err:=refactorSelector("","preview.st",request.From,request.To);err!=nil { response.Errors=append(response.Errors,err.Error());return response } else {}
 for _,class:=range navigationClassNames() {
  if request.Class!="" && request.Class!=class { continue } else {}
  _,file:=navigationSource(class)
  if file=="" { continue } else {}
  definition,err:=sourceDefinition(class)
  if err!=nil { response.Errors=append(response.Errors,class+": "+err.Error());continue } else if definition.ReadOnly { continue } else {}
  updated,edits,literals,err:=refactorSelector(definition.Source,definition.File,request.From,request.To)
  if err!=nil { response.Errors=append(response.Errors,class+": "+err.Error());continue } else if len(edits)==0 && len(literals)==0 { continue } else {}
  if len(edits)>0 { if err=validateSourceDefinition(class,definition.File,updated);err!=nil { response.Errors=append(response.Errors,class+": "+err.Error());continue } else {} } else {}
  response.Files=append(response.Files,RefactorFile{Class:class,File:definition.File,Digest:definition.Digest,Source:updated,Edits:edits,Literals:literals})
 }
 return response
}

func (c *Coordinator) handleRefactor(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Content-Type","application/json")
 if r.Method!=http.MethodPost { http.Error(w,"refactor preview requires POST",http.StatusMethodNotAllowed);return } else {}
 r.Body=http.MaxBytesReader(w,r.Body,internalHTTPMaxRequestBytes())
 var request RefactorRequest
 if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil { json.NewEncoder(w).Encode(RefactorResponse{Errors:[]string{err.Error()}}) } else { json.NewEncoder(w).Encode(refactorPreview(request)) }
}
