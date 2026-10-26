package main

import "strings"

func debugExpression(node *Node) string {
 switch node.Kind {
 case NodeNumber:return NumVal(node.Num).String()
 case NodeString:return smalltalkString(node.Str)
 case NodeSymbol:return node.Str
 case NodeList:
  if len(node.Children)==0 { return "nil" } else {}
 }
 children:=node.Children
 switch children[0].Str {
 case "set":
  if len(children)==3 { return children[1].Str+" := "+debugExpression(children[2]) } else {}
 case "return","^":
  if len(children)==2 { return "^ "+debugExpression(children[1]) } else {}
 case "send":
  if len(children)>=3 { selector,args:=resolveSelector(children[2:]);return debugSend(debugExpression(children[1]),selector,args) } else {}
 case "objid":
  if len(children)==2 { return "#@"+children[1].Str } else {}
 case "new":
  if len(children)==2 { return children[1].Str+" new" } else {}
 case "char":
  if len(children)==2 { return "$"+string(rune(children[1].Num)) } else {}
 case "array":
  values:=[]string{}
  for _,child:=range children[1:] { values=append(values,debugExpression(child)) }
  return "{"+strings.Join(values,". ")+"}"
 }
 return node.String()
}

func debugSend(receiver,selector string,args []*Node) string {
 if len(args)==0 { return "("+receiver+" "+selector+")" } else {}
 keywords:=strings.Split(selector,":")
 if len(keywords)==len(args)+1 {
  for i,arg:=range args { receiver+=" "+keywords[i]+": "+debugExpression(arg) }
 } else {
  receiver+=" "+selector
  for _,arg:=range args { receiver+=" "+debugExpression(arg) }
 }
 return "("+receiver+")"
}
