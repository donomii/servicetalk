package main

const objectWorkspaceJS = `const objectWorkspaceDrafts = new Map();
let objectWorkspaceReceiver = null;
let objectWorkspaceMethods = {};
let objectWorkspaceDescriptions = {};
function updateObjectWorkspace(id, inspection, metadata) {
  const draft = objectWorkspaceDrafts.get(id) || {source:'self', result:null, output:'', error:''};
  objectWorkspaceDrafts.set(id,draft);
  objectWorkspaceReceiver = id;
  objectWorkspaceMethods = inspection.methodSignatures || {};
  objectWorkspaceDescriptions = metadata.methods || {};
  document.getElementById('ow-code').value = draft.source;
  document.getElementById('ow-target').textContent = 'self → ' + inspection.className;
  const selector = document.getElementById('ow-method');
  const previous = selector.value;
  selector.replaceChildren();
  const prompt = document.createElement('option'); prompt.value='';prompt.textContent='Choose a method…';selector.appendChild(prompt);
  [...(inspection.methods || [])].sort().forEach(name=>{
    const option=document.createElement('option');option.value=name;option.textContent=name;selector.appendChild(option);
  });
  if ([...selector.options].some(option=>option.value===previous)) selector.value=previous; else {}
  workspaceArguments();
  const suggestions=document.getElementById('ow-suggestions');suggestions.replaceChildren();
  (metadata.properties || []).forEach(property=>{
    suggestions.appendChild(navButton('Read '+property.name,()=>{document.getElementById('ow-code').value='self '+property.getter;rememberWorkspaceDraft();runObjectWorkspace();}));
  });
  renderWorkspaceResult(draft);
}
function rememberWorkspaceDraft() {
  const draft=objectWorkspaceDrafts.get(objectWorkspaceReceiver);
  if(draft) draft.source=document.getElementById('ow-code').value; else {}
}
function workspaceArguments() {
  const selector=document.getElementById('ow-method').value;
  const metadata=objectWorkspaceDescriptions[selector] || {};
  const names=objectWorkspaceMethods[selector] || [];
  const count=names.length || (selector.match(/:/g)||[]).length || (selector && /^[^a-zA-Z0-9_]+$/.test(selector)?1:0);
  const container=document.getElementById('ow-args');container.replaceChildren();
  document.getElementById('ow-signature').textContent=metadata.returns?'Returns '+metadata.returns:'';
  for(let i=0;i<count;i++){
    const info=(metadata.arguments||[])[i]||{};
    const name=info.name||names[i]||'Argument '+(i+1);
    const row=document.createElement('label');row.className='marg';
    const label=document.createElement('span');label.textContent=name;
    const type=document.createElement('select');type.setAttribute('aria-label',name+' workspace type');
    ['number','string','bool','nil','objid','character','expression','result'].forEach(kind=>{const option=document.createElement('option');option.value=kind;option.textContent=kind;type.appendChild(option);});
    if([...type.options].some(option=>option.value===info.type))type.value=info.type;else{}
    const input=document.createElement('input');input.setAttribute('aria-label',name+' workspace argument');input.placeholder=name;
    type.onchange=()=>{input.disabled=type.value==='nil';input.placeholder=type.value==='expression'?'it or a Smalltalk expression':name;configureResultInput(type,input);};
    input.disabled=type.value==='nil';row.append(label,type,input);container.appendChild(row);
  }
}
function workspaceArgumentSource(row) {
  if(row.querySelector('select').value==='expression'){
    const source=row.querySelector('input').value.trim();
    if(!source)throw Error('Enter an argument expression.');else{return '('+source+')';}
  }else{}
  const value=argumentValue(row);
  switch(value.kind){
    case 'number':return String(value.num);
    case 'bool':return String(value.bool);
    case 'nil':return 'nil';
    case 'string':return "'"+value.str.replace(/'/g,"''")+"'";
    case 'objid':
      if(!/^[a-zA-Z0-9_]+$/.test(value.objid))throw Error('Use a valid object ID.');else{return '#@'+value.objid;}
    case 'character':return '$'+String.fromCodePoint(value.char);
  }
}
function insertWorkspaceMethod() {
  try{
    const selector=document.getElementById('ow-method').value;
    if(!selector)throw Error('Choose a method first.');else{}
    const args=[...document.querySelectorAll('#ow-args .marg')].map(workspaceArgumentSource);
    let expression='self '+selector;
    if(selector.includes(':')){expression='self '+selector.split(':').slice(0,-1).map((part,i)=>part+': '+args[i]).join(' ');}
    else if(args.length)expression+=' '+args[0];else{}
    const editor=document.getElementById('ow-code');
    if(editor.value.trim()==='self'){editor.value=expression;editor.setSelectionRange(0,expression.length);}else{editor.setRangeText(expression,editor.selectionStart,editor.selectionEnd,'select');}editor.focus();rememberWorkspaceDraft();
    document.getElementById('ow-result').textContent='';
  }catch(error){document.getElementById('ow-result').textContent=error.message;}
}
function renderWorkspaceResult(draft) {
  const result=document.getElementById('ow-result');result.replaceChildren();
  if(draft.error){result.textContent='Error: '+draft.error;return;}else{}
  if(draft.result)renderResult(result,draft.response||{result:draft.result,output:draft.output,handle:draft.handle});else{}
}
async function runObjectWorkspace() {
  const id=objectWorkspaceReceiver,draft=objectWorkspaceDrafts.get(id);
  if(!id || !draft)return;else{}
  rememberWorkspaceDraft();
  const button=document.getElementById('ow-run');
  if(button.disabled)return;else{}
  button.disabled=true;document.getElementById('ow-result').textContent='Running…';
  try{
    const result=await performInteraction({kind:'workspace',objectId:id,code:draft.source});
    if(result.error)throw Error(result.error);else{}
    draft.response=result;draft.handle=result.handle;
    draft.result=result.result;draft.output=result.output;draft.error='';
    if(inspectedId===id){await showInspect(id,true);renderWorkspaceResult(draft);}else{}
  }catch(error){draft.error=error.message;if(inspectedId===id)renderWorkspaceResult(draft);else{}}
  finally{button.disabled=false;}
}
document.getElementById('ow-code').addEventListener('keydown',event=>{if(event.ctrlKey && event.key==='Enter'){event.preventDefault();runObjectWorkspace();}else{}});
`
