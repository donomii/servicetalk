package main

const workbenchJS = `
let workbenchObject='',workbenchDefinition=null,exampleCatalog=[],exampleSelected='',exampleDrafts=new Map();
async function loadWorkbench(){
 try{const response=await fetch('/workbench').then(r=>r.json());if(response.error)throw Error(response.error);else{}
  workbenchObject=response.objectId;workbenchDefinition=response.definition;document.getElementById('workbench-name').textContent=response.definition.title==='servicetalk'?'Workbench':response.definition.title;
  const container=document.getElementById('workbench-tools');container.replaceChildren();
  for(const tool of response.definition.tools){const button=toolButton(tool.label,()=>runWorkbenchTool(tool));button.dataset.label=tool.label;button.dataset.action=tool.action;if(tool.action==='results')button.id='results-toggle';else if(tool.action==='record')button.id='record-toggle';else{}container.append(button);}
  updateResultChoices();const record=document.getElementById('record-toggle');if(record&&activeRecording)record.textContent='Stop recording';else{}
  document.getElementById('workbench-status').textContent='';
 }catch(error){document.getElementById('workbench-status').textContent=error.message;}
}
async function runWorkbenchTool(tool){
 switch(tool.action){
  case 'results':showResultShelf();break;
  case 'record':toggleRecording();break;
  case 'recording':showRecording();break;
  case 'methods':openNavigation();break;
  case 'objects':openNavigation('','objects');break;
  case 'trace':openTrace();break;
  case 'debugger':openDebugger();break;
  case 'wiring':openWiring();break;
  case 'examples':openExamples();break;
  case 'shell':openShellPanel();break;
  case 'services':openServices();break;
  case 'session':openSession();break;
  case 'inspect':showInspect(tool.objectId);break;
  case 'call':
   try{const response=await performInteraction({kind:'call',objectId:tool.objectId,method:tool.method,args:tool.args||[]});if(response.error)throw Error(response.error);else{}if(response.result.kind==='objid')await showInspect(response.result.objid);else showResultShelf();}catch(error){document.getElementById('workbench-status').textContent=error.message;}break;
 }
}
async function openWorkbench(reload=false){
 await loadWorkbench();document.getElementById('workbench-panel').hidden=false;
 startToolEditor(reload);document.getElementById('workbench-message').textContent='';
}
async function saveWorkbench(){
 try{const source=visualWorkbenchSource();const response=await performInteraction({kind:'call',objectId:workbenchObject,method:'definition:',args:[{kind:'string',str:source}]});if(response.error)throw Error(response.error);else{}toolEditorState.definition=JSON.parse(source);toolEditorState.rawText=source;await loadWorkbench();startToolEditor(false);document.getElementById('workbench-message').textContent='Saved';}catch(error){document.getElementById('workbench-message').textContent=error.message;}
}
async function resetWorkbench(){
 const response=await performInteraction({kind:'call',objectId:workbenchObject,method:'resetTools',args:[]});if(response.error){document.getElementById('workbench-message').textContent=response.error;}else{await openWorkbench(true);}
}
function inspectWorkbench(){closeToolEditor();showInspect(workbenchObject);}
function rememberExampleDraft(){
 if(!exampleSelected)return;else{}
 const draft={};for(const field of ['title','description','source','check'])draft[field]=document.getElementById('example-'+field).value;
 exampleDrafts.set(exampleSelected,draft);
}
async function openExamples(){
 rememberExampleDraft();document.getElementById('examples-panel').hidden=false;document.getElementById('examples-status').textContent='Loading examples…';
 try{const response=await fetch('/examples').then(r=>r.json());if(response.error)throw Error(response.error);else{}
  workbenchObject=response.workbench;exampleCatalog=response.examples;document.getElementById('examples-search').value='';renderExampleList();exampleSelected='';
  if(exampleCatalog.length)selectExample(exampleCatalog[0].objectId);else{}document.getElementById('examples-status').textContent='';
 }catch(error){document.getElementById('examples-status').textContent=error.message;}
}
function matchingExamples(query){return exampleCatalog.filter(example=>(example.title+' '+example.description+' '+example.source).toLowerCase().includes(query.toLowerCase()));}
function renderExampleList(){
 const list=document.getElementById('examples-list');list.replaceChildren();
 const query=document.getElementById('examples-search').value.trim(),matches=matchingExamples(query),editor=document.getElementById('example-editor');
 if(matches.length&&editor.hidden){editor.hidden=false;selectExample(matches[0].objectId);}else if(!matches.length)editor.hidden=true;else{}
 for(const example of matches)list.append(toolButton(example.title,()=>selectExample(example.objectId)));
 if(!matches.length)list.append(toolElement('p','No matching examples'));else{}
}
function selectExample(id){
 rememberExampleDraft();exampleSelected=id;const example=exampleCatalog.find(e=>e.objectId===id);if(!example)return;else{}
 const content=exampleDrafts.get(id)||example;for(const field of ['title','description','source','check'])document.getElementById('example-'+field).value=content[field];
 document.getElementById('example-result').replaceChildren();document.getElementById('examples-status').textContent='';showObservedExample(example);
}
async function saveExample(){
 if(!exampleSelected)return false;else{}rememberExampleDraft();const draft=exampleDrafts.get(exampleSelected),example=exampleCatalog.find(e=>e.objectId===exampleSelected);
 if(['title','description','source','check'].every(field=>draft[field]===example[field]))return true;else{}
 const args=['title','description','source','check'].map(field=>({kind:'string',str:draft[field]}));
 const response=await performInteraction({kind:'call',objectId:exampleSelected,method:'updateTitle:description:source:check:',args});if(response.error)throw Error(response.error);else{}
 Object.assign(example,draft);renderExampleList();document.getElementById('examples-status').textContent='Saved';return true;
}
async function runExample(){
 const button=document.getElementById('example-run'),id=exampleSelected;button.disabled=true;
 try{if(!await saveExample())return;else{}document.getElementById('examples-status').textContent='Running…';
  const response=await performInteraction({kind:'example',objectId:id});if(exampleSelected!==id)return;else{}
  renderResult(document.getElementById('example-result'),response);document.getElementById('examples-status').textContent=response.error?'Failed':document.getElementById('example-check').value.trim()?'Passed':'Completed (no check)';
 }catch(error){document.getElementById('examples-status').textContent=error.message;}finally{button.disabled=false;}
}
async function resetExample(){
 const id=exampleSelected;const response=await performInteraction({kind:'call',objectId:id,method:'reset',args:[]});
 if(response.error)document.getElementById('examples-status').textContent=response.error;else{const workspace=await inspectorCall(id,'workspace',[]);const example=exampleCatalog.find(e=>e.objectId===id);example.workspace=workspace.objid;showObservedExample(example);document.getElementById('example-result').replaceChildren();document.getElementById('examples-status').textContent='Workspace reset';}
}
async function newExample(){
 try{const response=await performInteraction({kind:'call',objectId:workbenchObject,method:'addExampleTitle:description:source:check:',args:[{kind:'string',str:'New example'},{kind:'string',str:''},{kind:'string',str:'42'},{kind:'string',str:"(it = 42) ifFalse: [ 'Expected 42' signal ]"}]});if(response.error)throw Error(response.error);else{}await openExamples();selectExample(response.result.objid);}catch(error){document.getElementById('examples-status').textContent=error.message;}
}
function inspectExample(workspace=false){const example=exampleCatalog.find(e=>e.objectId===exampleSelected);if(!example)return;else{}document.getElementById('examples-panel').hidden=true;showInspect(workspace?example.workspace:example.objectId);}
function closeExamples(){rememberExampleDraft();document.getElementById('examples-panel').hidden=true;}
document.getElementById('example-save').onclick=()=>saveExample().catch(error=>document.getElementById('examples-status').textContent=error.message);
loadWorkbench();
`
