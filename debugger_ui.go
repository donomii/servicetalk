package main

const debuggerJS = `
let debuggerTarget='',debuggerState=null,debuggerPoll=null,debuggerVersion=0;
function toolElement(tag,text='',className='') {const element=document.createElement(tag);element.textContent=text;element.className=className;return element;}
function toolButton(text,action) {const button=toolElement('button',text,'btn');button.type='button';button.onclick=action;return button;}
function valueEditor(label,value) {
 const row=toolElement('div','','marg');row.dataset.name=label;row.dataset.original=JSON.stringify(value);row.append(toolElement('label',label));
 const type=document.createElement('select');type.setAttribute('aria-label',label+' type');
 for(const kind of ['number','string','bool','nil','objid','character','result']) {const option=toolElement('option',kind);option.value=kind;type.append(option);}
 type.value=value.kind||'nil';
 const input=document.createElement('input');input.setAttribute('aria-label',label);
 input.value=value.kind==='string'?(value.str||''):value.kind==='objid'?value.objid:value.kind==='character'?String.fromCodePoint(value.char||0):fmtVal(value);
 input.disabled=type.value==='nil';type.onchange=()=>{input.disabled=type.value==='nil';configureResultInput(type,input);};
 row.append(type,input);return row;
}
async function debugAPI(request) {
 const response=await fetch('/debugger',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...request,objectId:debuggerTarget})});
 const result=await response.json();if(result.error)throw Error(result.error);else{}
 return result.snapshot;
}
function closeDebugger() {document.getElementById('debugger-panel').hidden=true;clearTimeout(debuggerPoll);debuggerVersion++;}
async function openDebugger(id=debuggerTarget,methods=[],signatures={}) {
 if(!id)return;else{}
 closeM();debuggerTarget=id;debuggerState=null;clearTimeout(debuggerPoll);const version=++debuggerVersion;
 const panel=document.getElementById('debugger-panel');panel.hidden=false;
 document.getElementById('debugger-target').textContent='Native method debugger · '+id;
 const select=document.getElementById('debugger-method');select.replaceChildren();
 try {
  if(!methods.length) {const response=await fetch('/relationships?id='+encodeURIComponent(id));const data=await response.json();methods=data.methods||[];signatures=data.methodSignatures||{};}else{}
  if(version!==debuggerVersion)return;else{}
  for(const method of [...methods].sort()) {const option=toolElement('option',method);option.value=method;select.append(option);}
  select.onchange=()=>{const target=document.getElementById('debugger-args');target.replaceChildren();for(const name of signatures[select.value]||[])target.append(valueEditor(name,{kind:'number',num:0}));};select.onchange();
  await refreshDebugger(version);
 }catch(error){document.getElementById('debugger-status').textContent=error.message;}
}
async function startDebugger() {
 const version=debuggerVersion;
 try {
  const method=document.getElementById('debugger-method').value;
  const args=[...document.querySelectorAll('#debugger-args .marg')].map(argumentValue);
  await debugAPI({action:'start',method,args});if(version!==debuggerVersion)return;else{}debuggerState=null;await refreshDebugger(version);
 }catch(error){document.getElementById('debugger-status').textContent=error.message;}
}
async function refreshDebugger(version=debuggerVersion) {
 clearTimeout(debuggerPoll);
 try {
  const snapshot=await debugAPI({action:'read'});
  if(version!==debuggerVersion || document.getElementById('debugger-panel').hidden)return;else{}
  const changed=!debuggerState || snapshot.id!==debuggerState.id || snapshot.revision!==debuggerState.revision || snapshot.state!==debuggerState.state;
  debuggerState=snapshot;
  document.getElementById('debugger-status').textContent=snapshot.state+(snapshot.method?' · '+snapshot.method:'')+(snapshot.error?'\n'+snapshot.error:'');
  const active=['queued','running','paused'].includes(snapshot.state);
  document.getElementById('debugger-start').disabled=active;
  document.getElementById('debugger-frame').hidden=!snapshot.id;
  document.getElementById('debugger-actions').querySelectorAll('button').forEach(button=>button.disabled=snapshot.state!=='paused');
  if(changed)await renderDebugFrame(snapshot,version);else{}
  if(active)debuggerPoll=setTimeout(()=>refreshDebugger(version),500);else{}
 }catch(error){if(version===debuggerVersion)document.getElementById('debugger-status').textContent=error.message;else{}}
}
async function renderDebugFrame(snapshot,version) {
 const statements=document.getElementById('debugger-statements');statements.replaceChildren();
 for(const [index,source] of (snapshot.statements||[]).entries()) {const line=toolElement('pre',(index+1)+'. '+source,index===snapshot.index?'debug-current':'');statements.append(line);}
 const args=document.getElementById('debugger-bindings');args.replaceChildren();
 for(const [name,value] of Object.entries(snapshot.arguments||{}))args.append(valueEditor(name,value));
 const fields=document.getElementById('debugger-fields');fields.replaceChildren();
 if(snapshot.state!=='idle') {
  const response=await fetch('/relationships?id='+encodeURIComponent(debuggerTarget));const data=await response.json();
  if(version!==debuggerVersion || snapshot.revision!==debuggerState.revision)return;else{}
  for(const [name,value] of Object.entries(data.instanceVars||{}))fields.append(valueEditor(name,value));
 }else{}
 for(const input of [...args.querySelectorAll('input,select'),...fields.querySelectorAll('input,select')])input.disabled=snapshot.state!=='paused'||(input.tagName==='INPUT'&&input.parentElement.querySelector('select').value==='nil');
 document.getElementById('debugger-code').value='';
 const result=document.getElementById('debugger-result');result.replaceChildren(toolElement('span','Result: '+fmtVal(snapshot.result)));
 if(snapshot.result?.kind==='objid')result.append(objectLink(snapshot.result.objid));else{}
 const returnValue=document.getElementById('debugger-return-value');returnValue.replaceChildren(valueEditor('Return value',snapshot.result||{kind:'nil'}));
}
async function commandDebugger(action) {
 if(!debuggerState)return;else{}
 try {
  const values=selector=>Object.fromEntries([...document.querySelectorAll(selector+' .marg')].map(row=>[row,argumentValue(row)]).filter(([row,value])=>JSON.stringify(value)!==row.dataset.original).map(([row,value])=>[row.dataset.name,value]));
  const command=action==='abort'?{id:debuggerState.id,revision:debuggerState.revision,action}:{id:debuggerState.id,revision:debuggerState.revision,action,code:document.getElementById('debugger-code').value,arguments:values('#debugger-bindings'),fields:values('#debugger-fields'),value:argumentValue(document.querySelector('#debugger-return-value .marg'))};
  await debugAPI({action:'command',command});await refreshDebugger(debuggerVersion);
 }catch(error){document.getElementById('debugger-status').textContent=error.message;}
}
document.getElementById('debugger-start').onclick=startDebugger;
for(const button of document.querySelectorAll('#debugger-actions button'))button.onclick=()=>commandDebugger(button.dataset.action);
`
