package main

const sessionCSS = `
.session-grid{display:grid;grid-template-columns:minmax(0,2fr) minmax(230px,1fr);gap:28px}.session-grid>section{min-width:0}.session-grid h4{font-size:15px;margin:16px 0 10px}.session-muted{color:var(--muted);font-size:12px}.session-form{display:flex;gap:10px;align-items:end;flex-wrap:wrap;margin:16px 0}.session-form label{display:flex;flex-direction:column;gap:5px;font-size:12px;min-width:0}.session-form input,.session-form select{max-width:100%;min-width:140px}.session-form .grow{flex:1}.session-person,.session-publication,.session-event{padding:12px 0;border-bottom:1px solid var(--line);overflow-wrap:anywhere}.session-publication>strong{display:block}.session-publication .result-actions{margin-bottom:0}.session-event{font-size:12px}.session-event>span{display:block;color:var(--muted)}#session-board{min-height:120px}#session-activity{max-height:380px;overflow:auto}#session-endpoint{display:block;font:12px var(--mono);overflow-wrap:anywhere;margin:8px 0}#session-membership{padding:12px 16px;background:var(--accent-soft);border-radius:6px;margin:14px 0}#session-button{font-size:12px;white-space:nowrap;max-width:240px;overflow:hidden;text-overflow:ellipsis}#session-name-label{font-size:12px;color:var(--muted);margin-left:12px}#session-message:empty{display:none}.session-value{font:13px var(--mono);white-space:pre-wrap;overflow-wrap:anywhere;max-height:120px;overflow:auto;margin:6px 0}@media(max-width:650px){.session-grid{grid-template-columns:minmax(0,1fr);gap:12px}#session-button{padding:5px 7px;max-width:160px}#session-name-label{margin:0}#connection-status{font-size:0}#connection-status::before{margin:0}.session-form label{width:100%}.session-form input,.session-form select{width:100%}}
`

const sessionHTML = `
<section id="session-panel" class="runtime-panel" hidden>
 <div class="runtime-toolbar"><strong>Shared session</strong><button class="btn" onclick="refreshSession()">Refresh</button><button class="btn" onclick="closeSession()">Close</button></div>
 <p class="session-muted">Work on the same live objects. Keep your own expression workspace and share results by name.</p>
 <div id="session-message" class="runtime-status" role="status"></div>
 <form id="session-join" class="session-form" onsubmit="joinSession(event)"><label class="grow">Your name<input id="session-name" required maxlength="80" placeholder="Name"></label><label>Joining as<select id="session-kind"><option value="human">Person</option><option value="agent">Agent</option></select></label><button class="btn btn-run">Join session</button></form>
 <div id="session-membership" hidden><span id="session-identity"></span> <button class="btn" onclick="leaveSession()">Leave session</button></div>
 <div class="session-grid"><section><h4>Shared objects and results</h4><div id="session-board"></div>
 <form id="session-publish" class="session-form" onsubmit="publishSessionResult(event)" hidden><label>Shared name<input id="session-share-name" required maxlength="120" placeholder="e.g. counter" oninput="resetPublicationRevision()"></label><label class="grow">Result<select id="session-share-result" required></select></label><button class="btn btn-run" id="session-share-submit">Share result</button></form><p id="session-replacing" class="session-muted"></p>
 <h4>Activity</h4><form id="session-note-form" class="session-form" hidden onsubmit="postSessionNote(event)"><label class="grow">Leave a note<input id="session-note" placeholder="What are you working on?" maxlength="2000" required></label><button class="btn">Post note</button></form><div id="session-activity"></div></section>
 <section><h4>Participants</h4><div id="session-people"></div><h4>Connect another client</h4><p class="session-muted">People open this workbench and join. Agents connect to the MCP endpoint and use the session tool to join.</p><code id="session-endpoint"></code><button class="btn" onclick="copySessionEndpoint()">Copy MCP address</button><div id="session-object-link" class="result-actions"></div></section></div>
</section>
`

const sessionJS = `
let sessionParticipant=null,sessionSnapshot=null,sessionReplacement=null,sessionRefreshBusy=false;
try{sessionParticipant=JSON.parse(sessionStorage.getItem('servicetalk-participant')||'null');}catch(error){sessionParticipant=null;}
function currentParticipantID(){return sessionParticipant?.id||'';}
function sessionMessage(text){document.getElementById('session-message').textContent=text;}
function saveSessionParticipant(value){sessionParticipant=value;if(value)sessionStorage.setItem('servicetalk-participant',JSON.stringify(value));else sessionStorage.removeItem('servicetalk-participant');renderSessionIdentity();}
function renderSessionIdentity(){
 const joined=!!sessionParticipant;document.getElementById('session-join').hidden=joined;document.getElementById('session-membership').hidden=!joined;document.getElementById('session-publish').hidden=!joined;document.getElementById('session-note-form').hidden=!joined;
 document.getElementById('session-identity').textContent=joined?'Joined as '+sessionParticipant.name:'';document.getElementById('session-button').textContent=joined?sessionParticipant.name+' · Session':'Join session';document.getElementById('session-button').title=joined?'Joined as '+sessionParticipant.name:'Join the shared session';document.getElementById('session-name-label').textContent=joined?sessionParticipant.name+'’s workspace':'';
}
async function sessionRequest(request){const result=await uiPost('/session',{...request,participantId:request.participantId||currentParticipantID()});if(result.error)throw Error(result.error);else{return result;}}
function closeSessionTools(){closeSession();closeServices();closeShellPanel();}
async function openSession(){closeServices();closeShellPanel();document.getElementById('session-panel').hidden=false;sessionMessage('');updateSessionResultChoices();await refreshSession();}
function closeSession(){document.getElementById('session-panel').hidden=true;}
async function refreshSession(){
 if(sessionRefreshBusy)return;else{}sessionRefreshBusy=true;
 try{const result=await fetch('/session').then(response=>response.json());if(result.error)throw Error(result.error);else{}sessionSnapshot=result;
  if(sessionParticipant){const found=result.participants?.find(item=>item.id===sessionParticipant.id&&item.active);if(found)saveSessionParticipant(found);else{saveSessionParticipant(null);sessionMessage('Your previous participation has ended. Join to continue.');}}else{}
  renderSessionSnapshot(result);
 }catch(error){sessionMessage(error.message);}finally{sessionRefreshBusy=false;}
}
async function joinSession(event){
 event.preventDefault();const button=event.submitter;button.disabled=true;
 try{const name=document.getElementById('session-name').value.trim(),kind=document.getElementById('session-kind').value,result=await sessionRequest({action:'join',name,kind,participantId:''});const participant=result.participants?.find(item=>item.id===result.participantId)||{id:result.participantId,name,kind,workspaceId:result.workspaceId,active:true};if(!participant.id)throw Error('Join did not return a participant.');else{}saveSessionParticipant(participant);sessionSnapshot=result;renderSessionSnapshot(result);sessionMessage('Joined. Your Playground now uses your workspace.');}catch(error){sessionMessage(error.message);}finally{button.disabled=false;}
}
async function leaveSession(){
 try{await sessionRequest({action:'leave'});saveSessionParticipant(null);sessionMessage('Left the session. Shared objects remain available.');await refreshSession();}catch(error){sessionMessage(error.message);}
}
function updateSessionResultChoices(selected){
 const select=document.getElementById('session-share-result');if(!select)return;else{}const previous=selected||select.value;select.replaceChildren();
 for(const [handle,result] of [...retainedResults].reverse()){const option=document.createElement('option');option.value=handle;option.textContent=(result.request?.label||result.request?.method||result.request?.code?.split('\n')[0]||'Result').slice(0,70)+' → '+fmtVal(result.result).slice(0,70);select.append(option);}
 if([...select.options].some(option=>option.value===previous))select.value=previous;else{}document.getElementById('session-share-submit').disabled=!select.options.length;
}
async function shareResultInSession(result){await openSession();updateSessionResultChoices(result.handle);if(!sessionParticipant){sessionMessage('Join, then give this result a shared name.');document.getElementById('session-name').focus();}else document.getElementById('session-share-name').focus();}
function resetPublicationRevision(){sessionReplacement=null;document.getElementById('session-replacing').textContent='';document.getElementById('session-share-submit').textContent='Share result';}
function replacePublication(publication){sessionReplacement={name:publication.name,revision:publication.revision};document.getElementById('session-share-name').value=publication.name;document.getElementById('session-replacing').textContent='Replacing '+publication.name+' at revision '+publication.revision;document.getElementById('session-share-submit').textContent='Replace shared result';updateSessionResultChoices();document.getElementById('session-share-result').focus();}
async function publishSessionResult(event){
 event.preventDefault();const name=document.getElementById('session-share-name').value.trim(),result=retainedResults.get(document.getElementById('session-share-result').value);if(!result){sessionMessage('Run an expression first, then choose its result.');return;}else{}
 const expectedRevision=sessionReplacement?.name===name?sessionReplacement.revision:0;event.submitter.disabled=true;
 try{sessionSnapshot=await sessionRequest({action:'publish',name,value:result.result,expectedRevision});renderSessionSnapshot(sessionSnapshot);resetPublicationRevision();sessionMessage('Shared as '+name+'.');}catch(error){sessionMessage(error.message);await refreshSession();}finally{event.submitter.disabled=false;}
}
async function useSessionPublication(publication){
 if(!sessionParticipant){sessionMessage('Join to use a shared name in your workspace.');return;}else{}
 try{const binding=/^[\p{L}_][\p{L}\p{N}_]*$/u.test(publication.name)&&!['self','super','true','false','nil','coordinator'].includes(publication.name)&&!publication.name.startsWith('__')?publication.name:'shared_'+publication.name.replace(/[^\p{L}\p{N}_]/gu,'_');sessionSnapshot=await sessionRequest({action:'bind',name:publication.name,binding});renderSessionSnapshot(sessionSnapshot);code.setRangeText(binding,code.selectionStart,code.selectionEnd,'select');closeSession();code.focus();}catch(error){sessionMessage(error.message);}
}
async function unpublishSessionValue(publication){try{sessionSnapshot=await sessionRequest({action:'unpublish',name:publication.name,expectedRevision:publication.revision});renderSessionSnapshot(sessionSnapshot);sessionMessage('Removed the shared name '+publication.name+'.');}catch(error){sessionMessage(error.message);await refreshSession();}}
function sessionObjectLink(id,label){const link=objectLink(id),known=objectEntries.find(item=>item.id===id);link.textContent=label||'Inspect '+resultClassName(known?.className);link.title=id;link.onclick=()=>{closeSession();showInspect(id);};return link;}
function renderSessionSnapshot(snapshot){
 const activeKey=document.activeElement?.dataset.sessionKey,board=document.getElementById('session-board');board.replaceChildren();
 for(const publication of snapshot.publications||[]){const row=toolElement('div','','session-publication');row.append(toolElement('strong',publication.name));const value=toolElement('div','','session-value');if(!publication.present)value.textContent='Shared name removed';else if(publication.value.kind==='objid')value.append(sessionObjectLink(publication.value.objid));else value.textContent=publication.value.kind==='string'?publication.value.str:fmtVal(publication.value);row.append(value,toolElement('div','Revision '+publication.revision+' · '+(publication.participantName||publication.participantId),'session-muted'));const actions=toolElement('div','','result-actions');for(const [label,action] of [['Use in Playground',()=>useSessionPublication(publication)],['Replace',()=>replacePublication(publication)],['Unshare',()=>unpublishSessionValue(publication)]]){const button=toolButton(label,action);button.disabled=!sessionParticipant||(!publication.present&&label!=='Replace');if(label==='Replace'&&!publication.present)button.textContent='Restore';else{}button.dataset.sessionKey=publication.name+':'+label;actions.append(button);}row.append(actions);board.append(row);}
 if(!board.children.length)board.append(toolElement('p','Share an expression result here so everyone can use it.','session-muted'));else{}
 const people=document.getElementById('session-people');people.replaceChildren();for(const participant of snapshot.participants||[]){if(!participant.active)continue;else{}const row=toolElement('div','','session-person');row.append(toolElement('strong',participant.name),toolElement('div',(participant.kind==='agent'?'Agent':'Person')+(participant.id===currentParticipantID()?' · you':''),'session-muted'));people.append(row);}if(!people.children.length)people.append(toolElement('p','No one has joined yet.','session-muted'));else{}
 const activity=document.getElementById('session-activity'),scroll=activity.scrollTop;activity.replaceChildren();for(const item of [...(snapshot.activity||[])].reverse().slice(0,100)){const row=toolElement('div','','session-event');row.append(toolElement('strong',(item.name||item.participantName||'Session')+' · '+item.action),toolElement('span',item.detail||''));activity.append(row);}activity.scrollTop=scroll;
 const link=document.getElementById('session-object-link');link.replaceChildren();if(snapshot.sessionId){link.append(sessionObjectLink(snapshot.sessionId,'Inspect session object'));}else{}
 if(activeKey){const button=[...board.querySelectorAll('button')].find(item=>item.dataset.sessionKey===activeKey);button?.focus({preventScroll:true});}else{}
}
async function postSessionNote(event){event.preventDefault();if(!sessionParticipant){sessionMessage('Join to post a note.');return;}else{}event.submitter.disabled=true;try{sessionSnapshot=await sessionRequest({action:'note',text:document.getElementById('session-note').value});document.getElementById('session-note').value='';renderSessionSnapshot(sessionSnapshot);sessionMessage('Note posted.');}catch(error){sessionMessage(error.message);}finally{event.submitter.disabled=false;}}
async function copySessionEndpoint(){try{await navigator.clipboard.writeText(location.origin+'/mcp');sessionMessage('MCP address copied.');}catch(error){sessionMessage('MCP address: '+location.origin+'/mcp');}}
const sessionButton=document.createElement('button');sessionButton.id='session-button';sessionButton.className='btn';sessionButton.onclick=openSession;document.getElementById('hdr').insertBefore(sessionButton,document.getElementById('connection-status'));
const sessionLabel=document.createElement('span');sessionLabel.id='session-name-label';document.querySelector('.page-heading').append(sessionLabel);
document.getElementById('session-endpoint').textContent=location.origin+'/mcp';renderSessionIdentity();
document.addEventListener('keydown',event=>{if(event.key==='Escape')closeSession();else{}});
setInterval(()=>{if(!document.getElementById('session-panel').hidden)refreshSession();else{}},2000);
if(sessionParticipant)refreshSession();else{}
`
