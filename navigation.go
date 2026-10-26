package main

import (
 "encoding/json"
 "fmt"
 "io/fs"
 "net/http"
 "os"
 "path/filepath"
 "sort"
 "strings"
)

func navigationClassNames() []string {
 names := map[string]bool{}
 collect := func(entries []fs.DirEntry) {
  for _, entry := range entries {
   name := entry.Name()
   if strings.HasSuffix(name, ".smt") || strings.HasSuffix(name, ".st") {
    names[strings.TrimSuffix(name, filepath.Ext(name))] = true
   } else if entry.IsDir() {
    names[name] = true
   } else { continue }
  }
 }
 for _, dir := range classPath {
  if entries, err := os.ReadDir(dir); err == nil { collect(entries) } else { continue }
 }
 if embeddedClasses != nil {
  if entries, err := fs.ReadDir(embeddedClasses, "."); err == nil { collect(entries) } else { /* Disk definitions remain available. */ }
 } else { /* Ordinary build. */ }
 result := []string{}
 for name := range names { result = append(result, name) }
 sort.Strings(result)
 return result
}

func navigationSource(name string) (string, string) {
 for _, suffix := range []string{".smt", ".st"} {
  if source, ok := embeddedClassSource(name + suffix); ok { return string(source), "embedded:" + name + suffix } else { continue }
 }
 for _, dir := range classPath {
  for _, suffix := range []string{".smt", ".st", "/" + name + ".smt"} {
   path := filepath.Join(dir, name + suffix)
   if source, err := os.ReadFile(path); err == nil { return string(source), path } else { continue }
  }
 }
 return "", ""
}

func navigationSends(nodes []*Node) []string {
 found := map[string]bool{}
 var visit func(*Node)
 visit = func(n *Node) {
  if n == nil || n.Kind != NodeList { return } else { /* Examine executable forms. */ }
  children := n.Children
  if len(children) >= 3 && children[0].Str == "send" {
   found[children[2].Str] = true
  } else if len(children) >= 3 && children[0].Str == "cascade" {
   for _, message := range children[2:] {
    if len(message.Children) > 0 { found[message.Children[0].Str] = true } else { continue }
   }
  } else if len(children) > 0 && children[0].Str == "quote" { return } else { /* Nested expressions can contain sends. */ }
  for _, child := range children { visit(child) }
 }
 for _, n := range nodes { visit(n) }
 result := []string{}
 for selector := range found { result = append(result, selector) }
 sort.Strings(result)
 return result
}

func navigationIndex() NavigationIndex {
 result := NavigationIndex{Methods: []NavigationMethod{}, Errors: []string{}}
 for _, name := range navigationClassNames() {
  source, file := navigationSource(name)
  if file == "" { continue } else { /* Only actual class sources are indexed. */ }
  class, err := loadClassRaw(name)
  if err != nil { result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", file, err)); continue } else { /* Keep other classes even when one fails. */ }
  for selector, method := range class.Methods {
   if class.Foreign != nil { continue } else { /* Foreign declarations are indexed below. */ }
   line := 1
   if len(method.Body) > 0 { line = method.Body[0].Line } else { /* Empty method. */ }
   result.Methods = append(result.Methods, NavigationMethod{Class: class.Name, Selector: selector, Params: method.Params, Sends: navigationSends(method.Body), Source: source, File: file, Line: line})
  }
  if class.Foreign != nil {
   for _, method := range class.Foreign.Methods {
    result.Methods = append(result.Methods, NavigationMethod{Class: class.Name, Selector: method.Selector, Sends: []string{}, Source: source, File: file, Line: 1, Foreign: true})
   }
  } else { /* Native definitions were indexed above. */ }
 }
 sort.Slice(result.Methods, func(i, j int) bool {
  a, b := result.Methods[i], result.Methods[j]
  if a.Class == b.Class { return a.Selector < b.Selector } else { return a.Class < b.Class }
 })
 return result
}

func (c *Coordinator) handleNavigation(w http.ResponseWriter, r *http.Request) {
 if r.Method != http.MethodGet { http.Error(w, "navigation requires GET", http.StatusMethodNotAllowed); return } else { /* Read-only browser index. */ }
 w.Header().Set("Content-Type", "application/json")
 json.NewEncoder(w).Encode(navigationIndex())
}

func (c *Coordinator) handleRelationships(w http.ResponseWriter, r *http.Request) {
 if r.Method != http.MethodGet { http.Error(w,"relationships requires GET",http.StatusMethodNotAllowed); return } else {}
 id := r.URL.Query().Get("id")
 if id == "" { http.Error(w,"relationships requires object id",http.StatusBadRequest); return } else {}
 item, err := c.inspectObject(id, false)
 if err != nil { http.Error(w,err.Error(),http.StatusBadGateway); return } else {}
 item.Retention = &RetentionInspection{}
 for _, root := range c.rootSnapshot() {
  if root == id { item.Retention.Root = true; break } else { continue }
 }
 w.Header().Set("Content-Type","application/json")
 json.NewEncoder(w).Encode(item)
}
