package main

const sourceHistoryJS = `
let sourceState=null,sourceGeneration=0,sourceHistorical=null;
const sourceDrafts=new Map();
function rememberSourceDraft(){if(sourceState)sourceDrafts.set(sourceState.class,{source:document.getElementById('source-editor').value,digest:sourceState.digest});else{}}
function closeSource(){rememberSourceDraft();document.getElementById('source-panel').hidden=true;sourceGeneration++;}
async function sourceAPI(request) {
 const response=await fetch('/source',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});
 const result=await response.json();if(result.error)throw Error(result.error);else{}return result;
}
async function openSource(className) {
 rememberSourceDraft();const generation=++sourceGeneration;sourceState=null;sourceHistorical=null;
 document.getElementById('source-panel').hidden=false;document.getElementById('source-title').textContent=className;
 document.getElementById('source-status').textContent='Reading source and Git history…';document.getElementById('source-preview').textContent='';
 document.getElementById('source-restore').hidden=true;
 try {
  const state=await sourceAPI({action:'read',class:className});if(generation!==sourceGeneration)return;else{}
  sourceState=state;const draft=sourceDrafts.get(className);document.getElementById('source-editor').value=draft?draft.source:state.source;
  if(draft)sourceState.digest=draft.digest;else{}
  document.getElementById('source-editor').readOnly=state.readOnly;
  document.getElementById('source-save').disabled=state.readOnly;
  document.getElementById('source-commit').disabled=state.readOnly;
  document.getElementById('source-title').textContent=className+' · '+state.file;
  renderSourceHistory(state);document.getElementById('source-status').textContent=state.gitError||'';
 }catch(error){if(generation===sourceGeneration)document.getElementById('source-status').textContent=error.message;else{}}
}
function renderSourceHistory(state) {
 const list=document.getElementById('source-history');list.replaceChildren();
 for(const revision of state.revisions||[]) {
  const time=new Date(Number(revision.timestamp)*1000).toLocaleString();
  const button=toolButton(revision.id.slice(0,8)+' · '+revision.subject+' · '+time,()=>loadSourceRevision(revision.id));list.append(button);
 }
 if(!state.revisions?.length)list.append(toolElement('p','No commits for this file.'));else{}
 const git=document.getElementById('source-git');git.replaceChildren();
 if(state.gitObject){const link=objectLink(state.gitObject);link.onclick=()=>{closeSource();closeNavigation();showInspect(state.gitObject);};git.append(toolElement('span','Git object '),link);}else{}
}
async function loadSourceRevision(revision) {
 if(!sourceState)return;else{}const generation=sourceGeneration;
 try {
  const state=await sourceAPI({action:'revision',class:sourceState.class,revision});if(generation!==sourceGeneration)return;else{}
  sourceHistorical=state.historicalSource;document.getElementById('source-preview').textContent=sourceHistorical;
  document.getElementById('source-restore').hidden=sourceState.readOnly;document.getElementById('source-status').textContent='Revision '+revision;
 }catch(error){if(generation===sourceGeneration)document.getElementById('source-status').textContent=error.message;else{}}
}
function restoreSourceRevision(){if(sourceHistorical!==null){document.getElementById('source-editor').value=sourceHistorical;rememberSourceDraft();document.getElementById('source-status').textContent='Revision copied into editor.';}else{}}
async function saveSource() {
 if(!sourceState)return;else{}const generation=sourceGeneration;
 try {
  const state=await sourceAPI({action:'save',class:sourceState.class,digest:sourceState.digest,source:document.getElementById('source-editor').value});if(generation!==sourceGeneration)return;else{}
  sourceState=state;sourceDrafts.delete(state.class);renderSourceHistory(state);
  document.getElementById('source-status').textContent='Saved for new '+state.class+' objects.'+(state.gitError?'\n'+state.gitError:'');
 }catch(error){if(generation===sourceGeneration)document.getElementById('source-status').textContent=error.message;else{}}
}
async function sourceDiff() {
 if(!sourceState)return;else{}const generation=sourceGeneration;
 try {
  const state=await sourceAPI({action:'diff',class:sourceState.class});if(generation!==sourceGeneration)return;else{}
  sourceHistorical=null;document.getElementById('source-restore').hidden=true;document.getElementById('source-preview').textContent=state.diff||'Working file matches the index.';
 }catch(error){if(generation===sourceGeneration)document.getElementById('source-status').textContent=error.message;else{}}
}
async function commitSource() {
 if(!sourceState)return;else{}const generation=sourceGeneration;
 try {
  if(document.getElementById('source-editor').value!==sourceState.source)throw Error('Save the editor before committing.');else{}
  const state=await sourceAPI({action:'commit',class:sourceState.class,digest:sourceState.digest,message:document.getElementById('source-message').value});if(generation!==sourceGeneration)return;else{}
  sourceState=state;renderSourceHistory(state);document.getElementById('source-status').textContent='Committed '+state.revisions[0]?.id;
 }catch(error){if(generation===sourceGeneration)document.getElementById('source-status').textContent=error.message;else{}}
}
`
