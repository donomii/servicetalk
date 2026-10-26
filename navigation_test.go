package main

import (
 "encoding/json"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "testing"
 "testing/fstest"
)

func TestNavigationSends(t *testing.T) {
 nodes, err := ParseSmalltalk("self foo. self bar: [self baz]. self foo; quux")
 if err != nil { t.Fatal(err) } else {}
 got := navigationSends(nodes)
 want := []string{"bar:","baz","foo","quux"}
 if !reflect.DeepEqual(got,want) { t.Fatalf("senders = %v, want %v",got,want) } else {}
}

func TestNavigationIndexSourcePrecedence(t *testing.T) {
 oldPath, oldEmbedded := classPath, embeddedClasses
 t.Cleanup(func(){ classPath = oldPath; embeddedClasses = oldEmbedded })
 dir := t.TempDir(); classPath = []string{dir}; embeddedClasses = nil
 sources := map[string]string{
  "Example.st":"class Example [ caller [ ^self target ] target [ ^1 ] ]",
  "Broken.st":"class Broken [",
 }
 for name, source := range sources {
  if err := os.WriteFile(filepath.Join(dir,name),[]byte(source),0600); err != nil { t.Fatal(err) } else {}
 }
 index := navigationIndex()
 if len(index.Methods)!=2 || len(index.Errors)!=1 { t.Fatalf("index: %+v",index) } else {}
 if index.Methods[0].Selector!="caller" || !reflect.DeepEqual(index.Methods[0].Sends,[]string{"target"}) { t.Fatalf("caller: %+v",index.Methods[0]) } else {}
 embeddedClasses = fstest.MapFS{"Example.st":{Data:[]byte("class Example [ embedded [ ^2 ] ]")}}
 index = navigationIndex()
 if len(index.Methods)!=1 || index.Methods[0].Selector!="embedded" || index.Methods[0].File!="embedded:Example.st" { t.Fatalf("embedded source: %+v",index) } else {}
 recorder := httptest.NewRecorder()
 (&Coordinator{}).handleNavigation(recorder,httptest.NewRequest("GET","/navigation",nil))
 var response NavigationIndex
 if err := json.Unmarshal(recorder.Body.Bytes(),&response); err != nil { t.Fatal(err) } else {}
 if len(response.Methods)!=1 { t.Fatalf("HTTP navigation: %+v",response) } else {}
}
