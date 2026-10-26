package main

import "path/filepath"

func (p *stParser) noteSelector(name,kind string,tokens []stToken) {
 if !p.recordSelectors { return } else {}
 site:=SelectorSite{Selector:name,Kind:kind,Spans:[]SelectorSpan{}}
 for _,token:=range tokens { site.Spans=append(site.Spans,SelectorSpan{Start:token.start,End:token.end,Line:token.line}) }
 p.selectorSites=append(p.selectorSites,site)
}

func (p *stParser) noteMethodSelector(name string,header []stToken) {
 if !p.recordSelectors || len(header)==0 { return } else {}
 if header[0].kind!=stKeyword { p.noteSelector(name,"definition",header[:1]);return } else {}
 tokens:=[]stToken{}
 for _,token:=range header { if token.kind==stKeyword { tokens=append(tokens,token) } else {} }
 p.noteSelector(name,"definition",tokens)
}

func (p *stParser) noteParsedSend(node *Node,name string,tokens []stToken) {
 if node.Kind==NodeList && len(node.Children)>=3 && node.Children[0].Str=="send" { p.noteSelector(name,"send",tokens) } else {}
}

func smalltalkSelectorSites(source,selector string) ([]SelectorSite,[]int,error) {
 tokens,err:=newStLexer(source).tokenize()
 if err!=nil { return nil,nil,err } else {}
 parser:=&stParser{toks:tokens,recordSelectors:true}
 literals:=[]int{}
 var visitLiteral func(*Node)
 visitLiteral=func(node *Node){if node.Kind==NodeString && node.Str==selector { literals=append(literals,node.Line) } else {};for _,child:=range node.Children { visitLiteral(child) }}
 for parser.peek().kind!=stEOF {
  if node,err:=parser.parseTopLevel();err!=nil { return nil,nil,err } else { visitLiteral(node) }
  for parser.peek().kind==stDot { parser.advance() }
 }
 return parser.selectorSites,literals,nil
}

func sexprSelectorSites(source,selector string) ([]SelectorSite,[]int,error) {
 parser:=&parser{src:[]rune(source),line:1,spans:map[*Node]SelectorSpan{}}
 nodes:=[]*Node{}
 for { parser.skipSpace();if parser.pos>=len(parser.src) { break } else {};node,err:=parser.expr();if err!=nil { return nil,nil,err } else { nodes=append(nodes,node) } }
 sites:=[]SelectorSite{};literals:=[]int{}
 var visit func(*Node)
 visit=func(node *Node){
  if node.Kind==NodeString && node.Str==selector { literals=append(literals,node.Line);return } else if node.Kind!=NodeList || len(node.Children)==0 { return } else {}
  children:=node.Children;head:=children[0].Str
  if head=="quote" || head=="vars" || head=="superclass" { return } else {}
  selected:=map[*Node]bool{}
  add:=func(token *Node,kind string){if token.Kind==NodeSymbol || token.Kind==NodeString { sites=append(sites,SelectorSite{Selector:token.Str,Kind:kind,Spans:[]SelectorSpan{parser.spans[token]}});selected[token]=true } else {}}
  switch head {
  case "method":if len(children)>=3 { add(children[1],"definition");selected[children[2]]=true } else {}
  case "block":if len(children)>=2 { selected[children[1]]=true } else {}
  case "send":if len(children)>=3 { add(children[2],"send") } else {}
  case "cascade":
   if len(children)<2 { return } else {}
   for _,message:=range children[2:] { if message.Kind==NodeList && len(message.Children)>0 { add(message.Children[0],"send");for _,argument:=range message.Children[1:] { visit(argument) };selected[message]=true } else {} }
  }
  for _,child:=range children { if !selected[child] { visit(child) } else {} }
 }
 for _,node:=range nodes { visit(node) }
 return sites,literals,nil
}

func selectorLocations(source,file,selector string) ([]SelectorSite,[]int,error) {
 if filepath.Ext(file)==".st" { return smalltalkSelectorSites(source,selector) } else { return sexprSelectorSites(source,selector) }
}
