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
  request={...request,participantId:request.participantId||(typeof currentParticipantID==='function'?currentParticipantID():'')};
  let captured=null,captureError='';
  if(['eval','workspace'].includes(request.kind)){try{captured=await captureInputBindings(request);}catch(error){captureError=error.message;}}else{}
  const result=await uiPost('/interaction',{...request,recordingId:activeRecording});
  result.captured=captured;result.captureError=captureError||result.captureError||'';
  result.request={...request,...(result.request||{})};
  if(result.handle){retainedResults.set(result.handle,result);updateResultChoices();}else{}
  if(result.recordingError)document.getElementById('recording-status').textContent='Recording: '+result.recordingError;else{}
  return result;
}
function updateResultChoices(){
  const button=document.getElementById('results-toggle');if(button)button.textContent=(button.dataset.label||'Results')+' ('+retainedResults.size+')';else{}
  if(typeof updateSessionResultChoices==='function')updateSessionResultChoices();else{}
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
function resultClassName(name){return {__array__:'Array',__dictionary__:'Dictionary',__process__:'Process',__block__:'Block'}[name]||name||'Object';}
async function presentResultObject(display,value,type){
  const known=objectEntries.find(item=>item.id===value.objid),object=objectLink(value.objid),identity=document.createElement('code');
  type.textContent=resultClassName(known?.className);object.textContent='Inspect '+type.textContent;object.className='btn result-inspect';object.title=value.objid;
  identity.className='result-identity';identity.textContent=value.objid;display.append(object,identity);
  try{
    const response=await fetch('/relationships?id='+encodeURIComponent(value.objid)),inspection=await response.json();if(!response.ok||inspection.error)throw Error(inspection.error||response.statusText);else{}
    if(!display.isConnected)return;else{}type.textContent=resultClassName(inspection.className);object.textContent='Inspect '+type.textContent;
    if(!['__array__','__dictionary__'].includes(inspection.className))return;else{}
    const entries=Object.entries(inspection.instanceVars||{});if(inspection.className==='__array__')entries.sort((a,b)=>Number(a[0].slice(1,-1))-Number(b[0].slice(1,-1)));else entries.sort((a,b)=>a[0].localeCompare(b[0]));
    type.textContent+=' · '+entries.length+' '+(entries.length===1?'item':'items');const list=document.createElement('div');list.className='result-collection';
    for(const [key,item] of entries.slice(0,80)){
      const row=document.createElement('div'),label=document.createElement('span'),content=document.createElement('div');row.className='result-entry';label.className='result-key';label.textContent=key;
      if(item.kind==='objid'){const link=objectLink(item.objid),entry=objectEntries.find(entry=>entry.id===item.objid);link.textContent='Inspect '+resultClassName(entry?.className);link.title=item.objid;content.append(link);}else content.textContent=item.kind==='string'?item.str:fmtVal(item);
      row.append(label,content);list.append(row);
    }
    if(entries.length>80){const rest=document.createElement('div');rest.className='result-collection-more';rest.textContent='Showing 80 of '+entries.length+' items';list.appendChild(rest);}else{}
    display.insertBefore(list,object);
  }catch(error){if(display.isConnected){const status=document.createElement('div');status.className='result-status';status.textContent='Preview unavailable: '+error.message;display.appendChild(status);}else{}}
}
function renderResult(container,result){
  container.replaceChildren();
  if(result.error){container.textContent='Error: '+result.error;return;}else{}
  if(result.output){const printed=document.createElement('pre');printed.className='result-output';printed.setAttribute('aria-label','Printed output');printed.textContent=result.output;container.appendChild(printed);}else{}
  const value=result.result,display=document.createElement('div');display.className='result-value';display.dataset.kind=value.kind;
  const type=document.createElement('div');type.className='result-type';type.textContent={number:'Number',string:'Text',bool:'Boolean',nil:'Nil',character:'Character',objid:'Object'}[value.kind]||'Result';display.appendChild(type);
  if(value.kind==='objid'){
    presentResultObject(display,value,type);
  }else{
    const content=document.createElement(value.kind==='string'?'pre':'div');content.className=value.kind==='string'?'result-text':'result-scalar';content.textContent=value.kind==='string'?(value.str||'(empty text)'):fmtVal(value);display.appendChild(content);
  }
  container.appendChild(display);
  if(!result.handle)return;else{}
  const bar=document.createElement('div');bar.className='result-actions';const status=document.createElement('span');status.className='result-status';status.setAttribute('role','status');
  bar.appendChild(navButton('Use in Playground',()=>{code.setRangeText((result.participantId||'')===currentParticipantID()?result.handle:valueExpression(result.result),code.selectionStart,code.selectionEnd,'select');closeM();closeExamples();closeSessionTools();document.getElementById('result-shelf').hidden=true;document.getElementById('recording-panel').hidden=true;code.focus();}));
  if(result.request&&['eval','call','workspace'].includes(result.request.kind)){const save=navButton('Save as example',async()=>{save.disabled=true;try{await saveInteractionExample(result,status);}finally{save.disabled=false;}});bar.appendChild(save);}else{}
  bar.appendChild(navButton('Share in session',()=>shareResultInSession(result)));
  const more=document.createElement('details');more.className='result-more';const summary=document.createElement('summary');summary.textContent='More actions';more.appendChild(summary);
  const extra=document.createElement('div');extra.className='result-actions';more.appendChild(extra);
  extra.appendChild(navButton('Pick as argument',()=>{pickedResult=result.handle;extra.querySelector('.picked-status').textContent='Selected';document.getElementById('result-shelf').hidden=true;}));
  const picked=document.createElement('span');picked.className='picked-status';extra.appendChild(picked);
  const name=document.createElement('input');name.placeholder='Name this result';name.setAttribute('aria-label','Name this result');
  extra.append(name,navButton('Save name',async()=>{try{const saved=await uiPost('/result',{action:'bind',handle:result.handle,name:name.value.trim(),participantId:result.participantId||''});if(saved.error)throw Error(saved.error);else{status.textContent='Saved as '+name.value.trim();}}catch(error){status.textContent=error.message;}}));
  extra.appendChild(navButton('Release',async()=>{try{const released=await uiPost('/result',{action:'release',handle:result.handle,participantId:result.participantId||''});if(released.error)throw Error(released.error);else{retainedResults.delete(result.handle);updateResultChoices();bar.remove();more.remove();status.textContent='Released';}}catch(error){status.textContent=error.message;}}));
  container.append(bar,more,status);
}
async function clearResults(){
  for(const handle of retainedResults.keys()){
    const response=await uiPost('/result',{action:'release',handle,participantId:retainedResults.get(handle)?.participantId||''});
    if(response.error)throw Error(response.error);else{}
  }
  retainedResults.clear();updateResultChoices();out.replaceChildren();starterHelp();
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
  [...retainedResults.values()].reverse().forEach(result=>{const card=document.createElement('div');card.className='result-card';const source=document.createElement('div');source.className='result-source';source.textContent=result.request?.code||result.request?.label||result.request?.method||'Result';const body=document.createElement('div');renderResult(body,result);card.append(source,body);list.appendChild(card);});
  if(!retainedResults.size){const empty=document.createElement('p');empty.className='result-empty';empty.textContent='Run an expression or call a method to keep its result here.';list.appendChild(empty);}else{}
}
`
