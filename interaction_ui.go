package main

const interactionJS = `const retainedResults=new Map();
let activeRecording='';
let lastRecording='';
let pickedResult='';
async function uiPost(path,request){
  const response=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});
  const value=await response.json();
  if(!response.ok)throw Error(value.error||response.statusText);else{return value;}
}
async function performInteraction(request){
  const result=await uiPost('/interaction',{...request,recordingId:activeRecording});
  if(result.handle){retainedResults.set(result.handle,result);updateResultChoices();}else{}
  if(result.recordingError)document.getElementById('recording-status').textContent='Recording: '+result.recordingError;else{}
  return result;
}
function updateResultChoices(){
  const button=document.getElementById('results-toggle');if(button)button.textContent=(button.dataset.label||'Results')+' ('+retainedResults.size+')';else{}
  const choices=document.getElementById('result-choices');choices.replaceChildren();
  [...retainedResults].reverse().forEach(([handle,result])=>{const option=document.createElement('option');option.value=handle;option.label=fmtVal(result.result);choices.appendChild(option);});
}
function configureResultInput(type,input){
  if(type.value==='result'){input.setAttribute('list','result-choices');input.value=pickedResult||[...retainedResults.keys()].pop()||'';input.placeholder='Choose a result';}
  else{input.removeAttribute('list');}
}
function retainedArgument(handle){
  const result=retainedResults.get(handle);
  if(!result)throw Error('Choose a retained result.');else{return result.result;}
}
function valueExpression(value){
  switch(value.kind){
    case 'string':return "'"+(value.str||'').replace(/'/g,"''")+"'";
    case 'number':return String(value.num||0);
    case 'bool':return String(value.bool||false);
    case 'nil':return 'nil';
    case 'character':return '$'+String.fromCodePoint(value.char||0);
    case 'objid':return '#@'+value.objid;
  }
}
function renderResult(container,result){
  container.replaceChildren();
  if(result.error){container.textContent='Error: '+result.error;return;}else{}
  if(result.output){const printed=document.createElement('pre');printed.textContent=result.output;container.appendChild(printed);}else{}
  if(result.result.kind==='objid')container.appendChild(objectLink(result.result.objid));
  else container.appendChild(document.createTextNode(fmtVal(result.result)));
  if(!result.handle)return;else{}
  const bar=document.createElement('div');bar.className='result-actions';
  bar.appendChild(navButton('Use in REPL',()=>{code.setRangeText(result.handle,code.selectionStart,code.selectionEnd,'select');closeM();document.getElementById('result-shelf').hidden=true;document.getElementById('recording-panel').hidden=true;code.focus();}));
  bar.appendChild(navButton('Pick as argument',()=>{pickedResult=result.handle;bar.querySelector('.picked-status').textContent='Selected';document.getElementById('result-shelf').hidden=true;}));
  const picked=document.createElement('span');picked.className='picked-status';bar.appendChild(picked);
  const name=document.createElement('input');name.placeholder='Name this result';name.setAttribute('aria-label','Name this result');
  const status=document.createElement('span');
  bar.append(name,navButton('Bind',async()=>{try{const saved=await uiPost('/result',{action:'bind',handle:result.handle,name:name.value.trim()});if(saved.error)throw Error(saved.error);else{status.textContent='Saved as '+name.value.trim();}}catch(error){status.textContent=error.message;}}),status);
  bar.appendChild(navButton('Release',async()=>{try{const released=await uiPost('/result',{action:'release',handle:result.handle});if(released.error)throw Error(released.error);else{retainedResults.delete(result.handle);updateResultChoices();bar.remove();}}catch(error){status.textContent=error.message;}}));
  container.appendChild(bar);
}
async function clearResults(){
  for(const handle of retainedResults.keys()){
    const response=await uiPost('/result',{action:'release',handle});
    if(response.error)throw Error(response.error);else{}
  }
  retainedResults.clear();updateResultChoices();out.replaceChildren();
}
async function toggleRecording(){
  const button=document.getElementById('record-toggle');
  try{
    if(activeRecording){lastRecording=activeRecording;activeRecording='';button.textContent='Record';await showRecording();}
    else{const recording=await uiPost('/recording',{action:'start'});if(recording.error)throw Error(recording.error);else{activeRecording=recording.objectId;lastRecording=activeRecording;button.textContent='Stop recording';document.getElementById('recording-status').textContent='Recording';}}
  }catch(error){document.getElementById('recording-status').textContent=error.message;}
}
async function showRecording(){
  const panel=document.getElementById('recording-panel');panel.hidden=false;
  const listing=document.getElementById('recording-list');listing.replaceChildren();
  if(!lastRecording){listing.textContent='No recording yet.';return;}else{}
  const recording=await uiPost('/recording',{action:'read',objectId:lastRecording});
  if(recording.error){listing.textContent=recording.error;return;}else{}
  const object=document.createElement('div');object.append('Recording object ',objectLink(lastRecording));listing.appendChild(object);
  recording.records.forEach((record,index)=>{const line=document.createElement('div');line.className='record-line';const request=record.request;line.textContent=(index+1)+'. '+(request.code||request.objectId+' '+request.method)+(record.response.error?' — '+record.response.error:' → '+fmtVal(record.response.result));listing.appendChild(line);});
  const editor=document.getElementById('recording-source');editor.value=recording.script;
  document.getElementById('recording-script').onclick=()=>{editor.value=recording.script;};
  document.getElementById('recording-test').onclick=()=>{editor.value=recording.test;};
  document.getElementById('recording-download').onclick=()=>{
    const url=URL.createObjectURL(new Blob([editor.value],{type:'text/plain'}));const link=document.createElement('a');link.href=url;link.download='recording.smt';link.click();URL.revokeObjectURL(url);
  };
  document.getElementById('recording-load').onclick=()=>{code.value=editor.value;panel.hidden=true;code.focus();};
  document.getElementById('recording-status').textContent=recording.records.length+' recorded interactions';
}
function showResultShelf(){
  const panel=document.getElementById('result-shelf');panel.hidden=false;const list=document.getElementById('result-shelf-list');list.replaceChildren();
  [...retainedResults.values()].reverse().forEach(result=>{const card=document.createElement('div');card.className='result-card';renderResult(card,result);list.appendChild(card);});
}
`
