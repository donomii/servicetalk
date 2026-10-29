package main

import (
 "bytes"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestTerminalAssetsServeEmbeddedFiles(t *testing.T) {
 for _,name:=range []string{"xterm.js","xterm.css","addon-fit.js","xterm.LICENSE","addon-fit.LICENSE","SOURCES.txt"} {
  path:="web/terminal/"+name;expected,err:=terminalAssets.ReadFile(path)
  if err!=nil { t.Fatal(err) } else {}
  response:=httptest.NewRecorder();serveTerminalAsset(response,httptest.NewRequest(http.MethodGet,"/"+path,nil))
  if response.Code!=http.StatusOK || !bytes.Equal(response.Body.Bytes(),expected) { t.Fatalf("embedded asset %s: status=%d bytes=%d",name,response.Code,response.Body.Len()) } else {}
  contentType:=response.Header().Get("Content-Type")
  if strings.HasSuffix(name,".css") && !strings.HasPrefix(contentType,"text/css") { t.Fatalf("CSS content type: %s",contentType) } else if strings.HasSuffix(name,".js") && !strings.Contains(contentType,"javascript") { t.Fatalf("JavaScript content type: %s",contentType) } else {}
 }
 missing:=httptest.NewRecorder();serveTerminalAsset(missing,httptest.NewRequest(http.MethodGet,"/web/terminal/missing.js",nil))
 if missing.Code!=http.StatusNotFound { t.Fatalf("missing asset status %d",missing.Code) } else {}
}

func TestSingleBinarySourceStagingIncludesTerminalAssets(t *testing.T) {
 directory:=t.TempDir()
 if err:=stageSource(".",directory);err!=nil { t.Fatal(err) } else {}
 for _,name:=range []string{"xterm.js","xterm.css","addon-fit.js","xterm.LICENSE","addon-fit.LICENSE","SOURCES.txt"} {
  path:=filepath.Join("web","terminal",name);expected,err:=os.ReadFile(path)
  if err!=nil { t.Fatal(err) } else {}
  actual,err:=os.ReadFile(filepath.Join(directory,path))
  if err!=nil || !bytes.Equal(actual,expected) { t.Fatalf("single-binary build omitted or changed %s: %v",path,err) } else {}
 }
}
