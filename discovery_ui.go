package main

const discoveryJS = `
function starterHelp(){
 const box=toolElement('div','','starter-help');box.id='starter-help';box.append(toolElement('strong','Explore ServiceTalk'),toolElement('p','Start with an example, find a live object, or browse methods.'));
 box.append(toolButton('Try an example',()=>openExamples()),toolButton('Find an object',()=>document.getElementById('object-filter').focus()),toolButton('Find a method',()=>openNavigation()));out.append(box);
}
function removeStarterHelp(){document.getElementById('starter-help')?.remove();}
function renderContextHelp(id,inspection,metadata){
 const help=document.getElementById('object-help');help.replaceChildren();const properties=metadata.properties||[];
 help.append(toolElement('span','Try'));
 for(const property of properties.slice(0,4))help.append(toolButton('Read '+property.name,async()=>{
  try{const response=await performInteraction({kind:'call',objectId:id,method:property.getter,args:[]});if(inspectedId!==id)return;else{}selectInspectorSection('state');renderResult(document.getElementById('mresult'),response);}catch(error){document.getElementById('mresult').textContent=error.message;}
 }));
 help.append(toolButton('Find examples',()=>findExamples(inspection.className)));
}
function renderSelectorHelp(selector){
 const help=document.getElementById('selector-help');help.replaceChildren();if(!selector)return;else{}
 help.append(toolButton('Find callers and implementations',()=>{closeM();openNavigation(selector);}));
 if(inspectorMethods.includes(selector))help.append(toolButton('Debug this method',async()=>{await openDebugger(inspectedId,inspectorMethods,methodSignatures);const select=document.getElementById('debugger-method');select.value=selector;select.onchange();}));else{
  const closest=inspectorMethods.map(name=>({name,score:selectorDistance(selector.toLowerCase(),name.toLowerCase())})).filter(item=>item.score<=Math.max(2,Math.floor(selector.length/3))).sort((a,b)=>a.score-b.score||a.name.localeCompare(b.name)).slice(0,3);
  if(closest.length)help.append(toolElement('span','Did you mean'));else{}
  for(const item of closest)help.append(toolButton(item.name,()=>{document.getElementById('mselector').value=item.name;prepareArguments();document.getElementById('mselector').focus();}));
 }
}
function selectorDistance(a,b){let previous=Array.from({length:b.length+1},(_,i)=>i);for(let i=1;i<=a.length;i++){const row=[i];for(let j=1;j<=b.length;j++)row[j]=Math.min(row[j-1]+1,previous[j]+1,previous[j-1]+(a[i-1]===b[j-1]?0:1));previous=row;}return previous[b.length];}
async function findExamples(query){await openExamples();document.getElementById('examples-search').value=query;renderExampleList();const found=matchingExamples(query)[0];if(found)selectExample(found.objectId);else document.getElementById('examples-status').textContent='No examples found for '+query;}
async function captureInputBindings(request){
 const names=new Set((request.code||'').match(/[\p{L}_][\p{L}\p{N}_]*/gu)||[]);if(!names.size)return {};else{}
 const coordinator=await fetch('/relationships?id=coordinator').then(response=>response.json());let main=null;
 for(const id of coordinator.runtime?.references||[]){const item=await fetch('/relationships?id='+encodeURIComponent(id)).then(response=>response.json());if(item.className==='__workspace__'){main=item;break;}else{}}
 if(!main)return {};else{}
 let fields=main.instanceVars||{};
 if(request.kind==='workspace'){
  const key='__inspectorWorkspace_'+[...new TextEncoder().encode(request.objectId)].map(byte=>byte.toString(16).padStart(2,'0')).join(''),object=fields[key];
  if(!object||object.kind!=='objid')return {};else{}
  const inner=await inspectorCall(object.objid,'bindings',[]),info=await fetch('/relationships?id='+encodeURIComponent(inner.objid)).then(response=>response.json());fields=info.instanceVars||{};
 }else{}
 return Object.fromEntries(Object.entries(fields).filter(([name,value])=>names.has(name)&&!name.startsWith('__')&&name!=='shellContext'&&value?.kind));
}
function captureExpression(request,inputBindings={}){
 const bindings={};
 function argument(value,index){if(value.kind==='objid'){const name='exampleArgument'+(index+1);bindings[name]=value;return name;}else{return valueExpression(value);}}
 if(request.kind==='eval')return {source:request.code,bindings:{...inputBindings}};else{}
 bindings.exampleReceiver={kind:'objid',objid:request.objectId};
 if(request.kind==='workspace'){const setup=Object.entries(inputBindings).map(([name,value],index)=>{const key='exampleBinding'+index;bindings[key]=value;return 'exampleWorkspace bindings setVar: '+valueExpression({kind:'string',str:name})+' to: '+key+'. ';}).join('');return {source:"exampleWorkspace := ObjectWorkspace new. exampleWorkspace target: exampleReceiver. "+setup+"exampleWorkspace evaluate: "+valueExpression({kind:'string',str:request.code})+". exampleWorkspace result",bindings};}else{}
 const args=(request.args||[]).map(argument),selector=request.method;
 if(selector.includes(':')){const parts=selector.split(':').slice(0,-1);if(parts.length!==args.length)throw Error('The selector and arguments do not match.');else{}return {source:'exampleReceiver '+parts.map((part,i)=>part+': '+args[i]).join(' '),bindings};}
 else if(args.length===1)return {source:'exampleReceiver '+selector+' '+args[0],bindings};
 else if(!args.length)return {source:'exampleReceiver '+selector,bindings};
 else throw Error('Cannot create source for this message.');
}
function observedCheck(value){
 if(value.kind==='objid')return '';else{}
 const literal=valueExpression(value);return '(it = '+literal+") ifFalse: [ 'Result differs from the observed value' signal ]";
}
async function saveInteractionExample(result,status){
 try{
  if(!workbenchObject)await loadWorkbench();else{}if(!workbenchObject)throw Error('Workbench is unavailable.');else{}
  const request=result.request,captured=captureExpression(request,result.captured||{}),title=request.kind==='call'?'Example: '+(request.label||request.method):request.kind==='workspace'?'Object workspace example':'Playground example';
  if(result.captureError)throw Error('Could not capture workspace bindings: '+result.captureError);else{}
  const description='';
  const args=[title,description,captured.source,''].map(str=>({kind:'string',str}));
  const example=await inspectorCall(workbenchObject,'addExampleTitle:description:source:check:',args);
  const bindings={...captured.bindings,__exampleObserved:result.result,__exampleObservedCheck:{kind:'string',str:observedCheck(result.result)}};
  for(const [name,value] of Object.entries(bindings))await inspectorCall(example.objid,'capture:value:',[{kind:'string',str:name},value]);
  await openExamples();selectExample(example.objid);document.getElementById('examples-status').textContent='Example saved. The result is observed; accept it as a check if it is what you expect.';
 }catch(error){status.textContent='Could not save example: '+error.message;}
}
async function showObservedExample(example){
 const section=document.getElementById('example-observed');section.replaceChildren();
 try{const value=await inspectorCall(example.workspace,'getVar:',[{kind:'string',str:'__exampleObserved'}]),check=await inspectorCall(example.workspace,'getVar:',[{kind:'string',str:'__exampleObservedCheck'}]);
  if(exampleSelected!==example.objectId||check.kind!=='string')return;else{}
  section.append(toolElement('strong','Observed result: '));if(value.kind==='objid')section.append(objectLink(value.objid));else section.append(document.createTextNode(fmtVal(value)));
  if(check.str)section.append(toolButton('Use observed result as check',()=>{document.getElementById('example-check').value=check.str;rememberExampleDraft();document.getElementById('examples-status').textContent='Check added. Save or run the example to keep it.';}));else{}
 }catch(error){if(exampleSelected===example.objectId)section.textContent='Could not read observed result: '+error.message;else{}}
}
starterHelp();
`
