package main

const cloningJS = `
let cloneTarget='',cloneSnapshot=null,cloneMethods={},cloneGeneration=0;
async function cloneInspectedObject(id) {
 const output=document.getElementById('mresult');output.textContent='Cloning…';
 try {
  const result=await performInteraction({kind:'call',objectId:id,method:'clone',args:[]});
  if(result.error)throw Error(result.error);else{}
  renderResult(output,result);if(result.result.kind==='objid')await showInspect(result.result.objid);else{}
 }catch(error){output.textContent=error.message;}
}
function closeClone(){document.getElementById('clone-panel').hidden=true;cloneGeneration++;}
async function openClone(id) {
 const generation=++cloneGeneration;cloneTarget=id;cloneSnapshot=null;cloneMethods={};
 document.getElementById('clone-panel').hidden=false;document.getElementById('clone-status').textContent='Reading loaded definition…';
 try {
  const reply=await inspectorCall(id,'__nativeSnapshotJson',[]);if(generation!==cloneGeneration)return;else{}
  cloneSnapshot=JSON.parse(reply.str);document.getElementById('clone-title').textContent='Derive from '+cloneSnapshot.definition.name+' · '+id;
  const fields=document.getElementById('clone-fields');fields.replaceChildren();for(const [name,value]of Object.entries(cloneSnapshot.fields||{}))fields.append(valueEditor(name,value));
  let definition=cloneSnapshot.definition;while(definition){for(const [name,method]of Object.entries(definition.methods||{}))if(!cloneMethods[name])cloneMethods[name]=method;else{}definition=definition.superclass;}
  const choices=document.getElementById('clone-method-choices');choices.replaceChildren();for(const name of Object.keys(cloneMethods).sort()){const option=document.createElement('option');option.value=name;choices.append(option);}
  document.getElementById('clone-method').value='';document.getElementById('clone-params').value='';document.getElementById('clone-code').value='';document.getElementById('clone-status').textContent='';
 }catch(error){if(generation===cloneGeneration)document.getElementById('clone-status').textContent=error.message;else{}}
}
function cloneMethodSelected(){const method=cloneMethods[document.getElementById('clone-method').value];if(method)document.getElementById('clone-params').value=(method.Params||[]).join(', ');else{}}
async function deriveClone() {
 if(!cloneSnapshot)return;else{}
 const generation=cloneGeneration;
 try {
  const fields=Object.fromEntries([...document.querySelectorAll('#clone-fields .marg')].map(row=>[row.dataset.name,argumentValue(row)]));
  const name=document.getElementById('clone-method').value.trim(),source=document.getElementById('clone-code').value;
  const params=document.getElementById('clone-params').value.split(',').map(name=>name.trim()).filter(Boolean);
  const methods=name?[{name,params,source}]:[];
  const response=await performInteraction({kind:'call',objectId:cloneTarget,method:'cloneWith:',args:[{kind:'string',str:JSON.stringify({fields,methods})}]});
  if(response.error)throw Error(response.error);else{}
  if(generation!==cloneGeneration)return;else{}
  closeClone();closeM();await showInspect(response.result.objid);
 }catch(error){if(generation===cloneGeneration)document.getElementById('clone-status').textContent=error.message;else{}}
}
`
