package main

const controlEditorJS = `
async function controlObjectChoices(input,className=''){
 const response=await fetch('/registry').then(r=>r.json());if(!input.isConnected)return;else{}
 const list=document.createElement('datalist');list.id='control-targets-'+Math.random().toString(36).slice(2);input.setAttribute('list',list.id);
 for(const item of (response.registry||[]).filter(o=>!className||o.className===className).sort((a,b)=>a.className.localeCompare(b.className)||a.id.localeCompare(b.id))){const option=toolElement('option',item.className+' · '+item.id);option.value=item.id;list.append(option);}input.after(list);
}
function selectedControlReceiver(){let node=viewEditorRoot(),receiver=node.receiver||viewEditorState.target;for(const index of viewEditorState.path){node=node.children[index];receiver=node.receiver||receiver;}return receiver;}
function renderControlIdentity(parent,node){
 if(node.controlId){const controls=toolElement('div','','runtime-toolbar');controls.append(toolElement('code',node.controlId),toolButton('Inspect control object',()=>{closeViewDesigner();showInspect(node.controlId);}));parent.append(controls);}else{parent.append(toolElement('p','New control — Save layout to create its object.'));}
 const receiver=visualField(parent,'Control receiver',node.receiver||'',value=>{if(value.trim())node.receiver=value.trim();else delete node.receiver;viewEditorChanged();renderViewDesigner();});receiver.placeholder='Inherit receiver';controlObjectChoices(receiver).catch(error=>receiver.title=error.message);
}
function renderControlAction(parent,node){
 const wired=!!node.wiring||node.action==='wiring';
 visualField(parent,'Action source',wired?'wiring':'message',value=>{node.action=value;if(value==='message')delete node.wiring;else{}viewEditorChanged();renderViewDesigner();},[{value:'message',label:'Send a message'},{value:'wiring',label:'Run Wiring graph'}]);
 if(wired){const graph=visualField(parent,'Wiring object',node.wiring,value=>{node.wiring=value.trim();viewEditorChanged();});controlObjectChoices(graph,'Wiring').catch(error=>graph.title=error.message);if(node.wiring)parent.append(toolButton('Open Wiring graph',()=>{closeViewDesigner();openWiring(node.wiring);}));else{}}
 else{const method=visualField(parent,'Action method',node.method,value=>{node.method=value;viewEditorChanged();});visualMethodChoices(method,selectedControlReceiver()).catch(error=>method.title=error.message);}
}
function copyControlTree(node){const copy=visualCopy(node);function clear(current){delete current.controlId;for(const child of current.children||[])clear(child);}clear(copy);return copy;}
function findControlPath(root,id,path=[]){if(root.controlId===id)return path;else{}for(const [index,child]of (root.children||[]).entries()){const found=findControlPath(child,id,[...path,index]);if(found)return found;else{}}return null;}
function receiveLiveControlView(view){
 if(!viewEditorState||document.getElementById('view-designer').hidden||viewEditorState.objectId!==view.objectId||viewEditorState.dirty||viewEditorState.raw)return;else{}
 if((document.getElementById('view-design-properties').contains(document.activeElement)&&document.activeElement.matches('input,textarea,select'))||JSON.stringify(viewEditorState.definition)===JSON.stringify(view.definition))return;else{}
 const selected=visualNodeAt(viewEditorRoot(),viewEditorState.path).controlId;viewEditorState.definition=visualCopy(view.definition);viewEditorState.path=findControlPath(viewEditorRoot(),selected)||[];renderViewDesigner();
}
async function openControlDesigner(id){
 try{const owner=await inspectorCall(id,'view',[]);if(owner.kind!=='objid')throw Error('This control is not attached to a view.');else{}
  const target=await inspectorCall(owner.objid,'target',[]),definition=await inspectorCall(owner.objid,'definition',[]);await showInspect(target.objid);openViewDesigner({objectId:owner.objid,definition:JSON.parse(definition.str)},target.objid);viewEditorState.path=findControlPath(viewEditorRoot(),id)||[];renderViewDesigner();
 }catch(error){document.getElementById('mresult').textContent=error.message;}
}
document.getElementById('view-design-json').oninput=()=>{viewEditorState.dirty=true;document.getElementById('view-designer-status').textContent='Unsaved changes';};
`
