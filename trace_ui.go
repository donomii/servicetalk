package main

const traceJS = `
let traceEvents=[];
function closeTrace(){document.getElementById('trace-panel').hidden=true;}
async function openTrace(){document.getElementById('trace-panel').hidden=false;await refreshTrace();}
async function configureTrace(action) {
 try {
  const response=await fetch('/trace',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})});
  const result=await response.json();if(result.errors?.length)throw Error(result.errors.join('\n'));else{}
  await refreshTrace();
 }catch(error){document.getElementById('trace-status').textContent=error.message;}
}
async function refreshTrace() {
 document.getElementById('trace-status').textContent='Reading trace…';
 try {
  const response=await fetch('/trace');const result=await response.json();traceEvents=result.events||[];
  document.getElementById('trace-status').textContent=(result.enabled?'Recording':'Stopped')+' · '+traceEvents.length+' messages'+(result.dropped?' · '+result.dropped+' older messages discarded':'')+(result.errors?.length?'\n'+result.errors.join('\n'):'');
  renderTrace();
 }catch(error){document.getElementById('trace-status').textContent=error.message;}
}
function traceObject(id) {const link=objectLink(id);link.onclick=()=>{closeTrace();showInspect(id);};return link;}
function renderTrace() {
 const filter=document.getElementById('trace-filter').value.toLowerCase();const chain=document.getElementById('trace-chain').value.trim();
 const list=document.getElementById('trace-list');list.replaceChildren();
 const events=traceEvents.filter(event=>(!chain||event.chainId===chain)&&(!filter||[event.method,event.sender,event.receiver,event.error||''].join(' ').toLowerCase().includes(filter)));
 for(const event of events) {
  const card=toolElement('article','','trace-event');
  const heading=toolElement('div','','trace-heading');heading.append(toolElement('strong',event.method),toolElement('span',event.state+' · '+event.durationMs.toFixed(2)+' ms'));
  const endpoints=toolElement('div','','trace-endpoints');endpoints.append(event.sender?traceObject(event.sender):toolElement('span','client'),toolElement('span',' → '),traceObject(event.receiver));
  const detail=toolElement('pre','Arguments: '+(event.args||[]).map(fmtVal).join(', ')+'\n'+(event.error?'Signal: '+event.error:'Result: '+fmtVal(event.result)));
  card.append(heading,endpoints,detail,toolButton('Follow chain',()=>{document.getElementById('trace-chain').value=event.chainId;renderTrace();}));
  if(event.result?.kind==='objid')card.append(traceObject(event.result.objid));else{}
  const metadata=toolElement('small',event.started+' · '+event.id);card.append(metadata);list.append(card);
 }
 if(!events.length)list.append(toolElement('p','No matching messages.'));else{}
}
function exportTrace() {
 const url=URL.createObjectURL(new Blob([JSON.stringify(traceEvents,null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download='servicetalk-trace.json';link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
}
`
