package main

const workbenchEditorJS = `
let toolEditorState=null;
function toolEditorChanged(){document.getElementById('workbench-message').textContent='Unsaved changes';renderToolOutline();renderToolPreview();}
function startToolEditor(reload){
 if(reload||!toolEditorState)toolEditorState={definition:visualCopy(workbenchDefinition),selected:0,raw:false};else{}
 const raw=toolEditorState.raw,rawText=toolEditorState.rawText;
 const container=document.getElementById('workbench-visual');container.replaceChildren();
 visualField(container,'Workbench title',toolEditorState.definition.title,value=>{toolEditorState.definition.title=value;toolEditorChanged();});
 const columns=toolElement('div','');columns.className='visual-columns';
 const list=toolElement('section','');list.append(toolElement('h4','Tools'));const outline=toolElement('nav','');outline.id='tool-outline';list.append(outline,toolButton('Add tool',addVisualTool));
 const properties=toolElement('section','');properties.id='tool-properties';columns.append(list,properties);container.append(columns);
 const preview=toolElement('section','');preview.append(toolElement('h4','Toolbar preview'));const buttons=toolElement('div','');buttons.id='tool-preview';buttons.className='runtime-toolbar';preview.append(buttons);container.append(preview);
 document.getElementById('workbench-editor').value=raw&&rawText!==undefined?rawText:JSON.stringify(toolEditorState.definition,null,2);toolEditorState.raw=raw;showToolEditorMode();renderToolOutline();renderToolProperties();renderToolPreview();
}
function closeToolEditor(){toolEditorState.rawText=document.getElementById('workbench-editor').value;document.getElementById('workbench-panel').hidden=true;}
function renderToolOutline(){
 const list=document.getElementById('tool-outline');list.replaceChildren();
 toolEditorState.definition.tools.forEach((tool,index)=>{const button=toolButton(tool.label||'Untitled tool',()=>selectVisualTool(index));button.setAttribute('aria-pressed',String(index===toolEditorState.selected));list.append(button);});
}
function renderToolPreview(){
 const preview=document.getElementById('tool-preview');preview.replaceChildren();
 toolEditorState.definition.tools.forEach((tool,index)=>preview.append(toolButton(tool.label||'Untitled tool',()=>selectVisualTool(index))));
}
function selectVisualTool(index){if(!visualValid(document.getElementById('workbench-panel')))return;else{}toolEditorState.selected=index;renderToolOutline();renderToolProperties();}
function addVisualTool(){
 if(!visualValid(document.getElementById('workbench-panel')))return;else{}
 const tools=toolEditorState.definition.tools;if(tools.length>=40){document.getElementById('workbench-message').textContent='The workbench supports at most 40 tools.';return;}else{}
 let count=1;while(tools.some(tool=>tool.id==='tool'+count))count++;tools.push({id:'tool'+count,label:'New tool',action:'methods'});toolEditorState.selected=tools.length-1;toolEditorChanged();renderToolProperties();
}
function moveVisualTool(delta){if(!visualValid(document.getElementById('workbench-panel')))return;else{}const list=toolEditorState.definition.tools,index=toolEditorState.selected,next=index+delta;if(next<0||next>=list.length)return;else{}[list[index],list[next]]=[list[next],list[index]];toolEditorState.selected=next;toolEditorChanged();renderToolProperties();}
function renderToolProperties(){
 const container=document.getElementById('tool-properties');container.replaceChildren();const tool=toolEditorState.definition.tools[toolEditorState.selected];if(!tool){container.textContent='Add a tool to begin.';return;}else{}
 container.append(toolElement('h4','Selected tool'));
 visualField(container,'Tool label',tool.label,value=>{tool.label=value;toolEditorChanged();});
 visualField(container,'Tool identifier',tool.id,value=>{tool.id=value;toolEditorChanged();});
 visualField(container,'Tool action',tool.action,value=>{tool.action=value;if(!['inspect','call'].includes(value)){delete tool.objectId;delete tool.method;delete tool.args;}else{}toolEditorChanged();renderToolProperties();},Object.entries(visualActions).map(([value,label])=>({value,label})));
 if(['inspect','call'].includes(tool.action))renderToolTarget(container,tool);else{}
 const controls=toolElement('div','');controls.className='runtime-toolbar';
 const up=toolButton('Move tool up',()=>moveVisualTool(-1)),down=toolButton('Move tool down',()=>moveVisualTool(1));up.disabled=toolEditorState.selected===0;down.disabled=toolEditorState.selected===toolEditorState.definition.tools.length-1;
 controls.append(up,down,toolButton('Remove tool',()=>{toolEditorState.definition.tools.splice(toolEditorState.selected,1);toolEditorState.selected=Math.max(0,toolEditorState.selected-1);toolEditorChanged();renderToolProperties();}));container.append(controls);
}
async function renderToolTarget(container,tool){
 const target=visualField(container,'Target object',tool.objectId,value=>{tool.objectId=value;toolEditorChanged();renderToolProperties();});
 try{const registry=await fetch('/registry').then(r=>r.json());if(!target.isConnected)return;else{}
  const list=document.createElement('datalist');list.id='tool-targets';target.setAttribute('list',list.id);
  for(const object of (registry.registry||[]).sort((a,b)=>a.className.localeCompare(b.className)||a.id.localeCompare(b.id))){const option=toolElement('option',object.className+' · '+object.id);option.value=object.id;list.append(option);}target.after(list);
 }catch(error){container.append(toolElement('p',error.message));}
 if(!target.isConnected||tool.action!=='call')return;else{}
 const method=visualField(container,'Tool method',tool.method,value=>{tool.method=value;toolEditorChanged();});let info=null;
 try{info=await visualMethodChoices(method,tool.objectId);}catch(error){container.append(toolElement('p',error.message));}
 if(!method.isConnected)return;else{}
 const argumentsContainer=toolElement('div','');container.append(argumentsContainer);
 const render=()=>{argumentsContainer.replaceChildren();visualArguments(argumentsContainer,tool,toolEditorChanged,(info?.methodSignatures||{})[tool.method]||[]);};render();
 container.append(toolButton('Match method arguments',()=>{const names=(info?.methodSignatures||{})[method.value];const count=names?names.length:(method.value.match(/:/g)||[]).length||(/^[^\w]+$/.test(method.value)?1:0);tool.args=Array.from({length:count},(_,index)=>(tool.args||[])[index]||{kind:'nil'});render();toolEditorChanged();}));
}
function showToolEditorMode(){document.getElementById('workbench-visual').hidden=toolEditorState.raw;document.getElementById('workbench-editor').hidden=!toolEditorState.raw;document.getElementById('tool-mode').textContent=toolEditorState.raw?'Visual editor':'Edit JSON';}
function validateVisualTools(definition){if(!definition||typeof definition.title!=='string'||!Array.isArray(definition.tools)||definition.tools.some(tool=>!tool||typeof tool!=='object'))throw Error('Workbench needs a title and tools array.');else{}return definition;}
function toggleToolEditor(){
 try{if(toolEditorState.raw){toolEditorState.definition=validateVisualTools(JSON.parse(document.getElementById('workbench-editor').value));toolEditorState.selected=0;toolEditorState.raw=false;startToolEditor(false);toolEditorChanged();}else{if(!visualValid(document.getElementById('workbench-panel')))return;else{}document.getElementById('workbench-editor').value=JSON.stringify(toolEditorState.definition,null,2);toolEditorState.raw=true;showToolEditorMode();}}catch(error){document.getElementById('workbench-message').textContent=error.message;}
}
function visualWorkbenchSource(){if(!visualValid(document.getElementById('workbench-panel')))throw Error('Correct the highlighted field before saving.');else{}return toolEditorState.raw?JSON.stringify(validateVisualTools(JSON.parse(document.getElementById('workbench-editor').value))):JSON.stringify(toolEditorState.definition);}
`
