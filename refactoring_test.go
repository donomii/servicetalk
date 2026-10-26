package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestSelectorRenamePreservesBindingsLiteralsAndComments(t *testing.T) {
 source:=`"value 値 stays in this comment"
class RenameProbe [ | value |
 initialize [ value := 5 ]
 value [ ^ value ]
 read [ ^ self value ]
 cascade [ ^ self value; value ]
 shadow [ ^ [ :value | value + self value ] value: 2 ]
 literal [ ^ #value ]
 text [ ^ 'value' ]
]`
 updated,edits,literals,err:=refactorSelector(source,"RenameProbe.st","value","amount")
 if err!=nil { t.Fatal(err) } else {}
 if len(edits)!=5 || len(literals)!=2 { t.Fatalf("selector sites: edits=%+v literals=%v",edits,literals) } else {}
 for _,preserved:=range []string{`"value 値 stays in this comment"`,`| value |`,`value := 5`,`^ value`,` :value | value + self amount`,`#value`,`'value'`} {
  if !strings.Contains(updated,preserved) { t.Fatalf("changed non-selector %q:\n%s",preserved,updated) } else {}
 }
 if !strings.Contains(updated,"self amount; amount") { t.Fatalf("lost cascade rename: %s",updated) } else {}
}

func TestSelectorRenameKeywordPartsAndSExpressions(t *testing.T) {
 smalltalk:=`class KeywordProbe [ take: a with: b [ ^ a + b ] run [ ^ self take: (self take: 1 with: 2) "preserved" with: 3 ] ]`
 updated,edits,_,err:=refactorSelector(smalltalk,"KeywordProbe.st","take:with:","sum:plus:")
 if err!=nil || len(edits)!=3 || !strings.Contains(updated,`self sum: (self sum: 1 plus: 2) "preserved" plus: 3`) { t.Fatalf("keyword rename: %s %+v %v",updated,edits,err) } else {}
 sexpr:=`; read stays in this comment
(class SProbe (vars read) (method read () read) (method invoke () (cascade self (read) (read))) (method text () "read") (method quoted () (quote (send self read))))`
 updated,edits,literals,err:=refactorSelector(sexpr,"SProbe.smt","read","amount")
 if err!=nil || len(edits)!=3 || len(literals)!=1 || !strings.Contains(updated,"(vars read)") || !strings.Contains(updated,"(quote (send self read))") || !strings.Contains(updated,"(cascade self (amount) (amount))") { t.Fatalf("sexpr rename: %s %+v %v %v",updated,edits,literals,err) } else {}
 binary:=`class BinaryProbe [ + n [ ^ n ] run [ ^ self + 1; + 2 ] ]`
 _,edits,_,err=refactorSelector(binary,"BinaryProbe.st","+","*")
 if err!=nil || len(edits)!=3 { t.Fatalf("binary selectors: %+v %v",edits,err) } else {}
}

func TestSelectorRenameRejectsArityAndCollisions(t *testing.T) {
 source:="class Probe [ read [ ^ 1 ] amount [ ^ 2 ] ]"
 for _,target:=range []string{"read:","+","amount","read","bad name"} {
  if _,_,_,err:=refactorSelector(source,"Probe.st","read",target);err==nil { t.Fatalf("accepted conflicting rename %s",target) } else {}
 }
}

func TestSelectorRenameLiteralArraysAndParameterLists(t *testing.T) {
 source:=`class LiteralProbe [ read [ ^ #(read 'read' #read) ] ]`
 updated,edits,literals,err:=refactorSelector(source,"LiteralProbe.st","read","amount")
 if err!=nil || len(edits)!=1 || len(literals)!=3 || !strings.Contains(updated,"#(read 'read' #read)") { t.Fatalf("literal references: %s %+v %v %v",updated,edits,literals,err) } else {}
 source=`(class Params (vars send self read) (method read (send self read) (block (send self read) (send self read))))`
 updated,edits,_,err=refactorSelector(source,"Params.smt","read","amount")
 if err!=nil || len(edits)!=2 || !strings.Contains(updated,"(method amount (send self read) (block (send self read) (send self amount)))") { t.Fatalf("binding lists: %s %+v %v",updated,edits,err) } else {}
 if _,_,_,err=refactorSelector("(class Incomplete (method read () (cascade)))","Incomplete.smt","read","amount");err!=nil { t.Fatal(err) } else {}
}

func TestSelectorRefactorPreviewSaveAndStaleEdit(t *testing.T) {
 dir:=t.TempDir()
 source:="class RefactorProbe [ read [ ^ 7 ] twice [ ^ self read + self read ] ]"
 if err:=os.WriteFile(filepath.Join(dir,"RefactorProbe.st"),[]byte(source),0600);err!=nil { t.Fatal(err) } else {}
 broker:=startNATSTestServerOnPort(t,-1);t.Cleanup(broker.Shutdown)
 base:=startMCPTestCoordinator(t,broker.ClientURL(),dir)
 body,_:=json.Marshal(RefactorRequest{From:"read",To:"amount",Class:"RefactorProbe"})
 var preview RefactorResponse
 if err:=json.Unmarshal(interactionTestPost(t,base,"/refactor",body),&preview);err!=nil { t.Fatal(err) } else {}
 if len(preview.Errors)!=0 || len(preview.Files)!=1 || len(preview.Files[0].Edits)!=3 { t.Fatalf("preview: %+v",preview) } else {}
 original,err:=os.ReadFile(filepath.Join(dir,"RefactorProbe.st"))
 if err!=nil || string(original)!=source { t.Fatal("preview wrote source") } else {}
 file:=preview.Files[0]
 saved:=sourceTestRequest(t,base,SourceRequest{Action:"save",Class:file.Class,Digest:file.Digest,Source:file.Source})
 if saved.Error!="" { t.Fatal(saved.Error) } else {}
 if value:=sourceTestEval(t,base,"probe := RefactorProbe new. probe twice");value!="14" { t.Fatalf("renamed dispatch: %s",value) } else {}
 stale:=sourceTestRequest(t,base,SourceRequest{Action:"save",Class:file.Class,Digest:file.Digest,Source:source})
 if stale.Error=="" { t.Fatal("stale preview overwrote source") } else {}
}
