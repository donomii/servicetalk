package main

const viewEditorJS = `
let viewEditorState=null;
const viewEditorDrafts=new Map();
const viewEditorRawDrafts=new Map();
function openViewDesigner(view,id){
 viewEditorState={objectId:view.objectId,target:id,definition:visualCopy(viewEditorDrafts.get(view.objectId)||view.definition),path:[],raw:false};
 document.getElementById('view-designer').hidden=false;document.getElementById('view-designer-status').textContent='';renderViewDesigner();if(viewEditorRawDrafts.has(view.objectId)){toggleViewDesignerJSON();document.getElementById('view-design-json').value=viewEditorRawDrafts.get(view.objectId);}else{}
}
function closeViewDesigner(){if(viewEditorState){viewEditorDrafts.set(viewEditorState.objectId,visualCopy(viewEditorState.definition));if(viewEditorState.raw)viewEditorRawDrafts.set(viewEditorState.objectId,document.getElementById('view-design-json').value);else viewEditorRawDrafts.delete(viewEditorState.objectId);}else{}document.getElementById('view-designer').hidden=true;}
function viewEditorRoot(){return viewEditorState.definition.body||viewEditorState.definition;}
function viewEditorChanged(){viewEditorDrafts.set(viewEditorState.objectId,visualCopy(viewEditorState.definition));document.getElementById('view-designer-status').textContent='Unsaved changes';renderViewOutline();previewViewDesigner();}
function renderViewDesigner(){
 const properties=document.getElementById('view-design-properties');properties.replaceChildren();
 const title=visualField(properties,'View title',viewEditorState.definition.title,value=>{viewEditorState.definition.title=value;viewEditorChanged();});
 title.placeholder='View title';const node=visualNodeAt(viewEditorRoot(),viewEditorState.path);properties.append(toolElement('h4','Selected control'));
 visualField(properties,'Control kind',node.kind||'column',value=>{node.kind=value;if(['row','column'].includes(value)&&!node.children)node.children=[];else{}viewEditorChanged();renderViewDesigner();},visualKinds);
 visualField(properties,'Control label',node.label,value=>{node.label=value;viewEditorChanged();});
 renderViewControlProperties(properties,node);
 document.getElementById('view-design-visual').hidden=viewEditorState.raw;document.getElementById('view-design-json').hidden=!viewEditorState.raw;document.getElementById('view-design-mode').textContent=viewEditorState.raw?'Visual editor':'Edit JSON';
 renderViewOutline();previewViewDesigner();
}
function renderViewOutline(){
 const tree=document.getElementById('view-design-tree');tree.replaceChildren();
 function append(node,path){const label=(node.kind||'column')+(node.label?' · '+node.label:node.text?' · '+node.text:node.getter?' · '+node.getter:'');const button=toolButton(label,()=>selectViewControl(path));button.style.paddingLeft=(8+path.length*14)+'px';button.setAttribute('aria-pressed',String(JSON.stringify(path)===JSON.stringify(viewEditorState.path)));button.setAttribute('aria-label','Select '+label);tree.append(button);if(['row','column'].includes(node.kind||'column'))(node.children||[]).forEach((child,index)=>append(child,[...path,index]));else{}}
 append(viewEditorRoot(),[]);
 const root=viewEditorState.path.length===0;for(const id of ['view-control-up','view-control-down','view-control-out','view-control-remove','view-control-copy'])document.getElementById(id).disabled=root;
}
function selectViewControl(path){if(!visualValid(document.getElementById('view-designer')))return;else{}viewEditorState.path=path;renderViewDesigner();}
function renderViewControlProperties(parent,node){
 const kind=node.kind||'column';
 if(['text','code','action','value'].includes(kind))visualField(parent,kind==='action'?'Button text':'Literal text',node.text,value=>{node.text=value;viewEditorChanged();});else{}
 if(!['row','column','action'].includes(kind)){const getter=visualField(parent,'Getter method',node.getter,value=>{node.getter=value;viewEditorChanged();});visualMethodChoices(getter,viewEditorState.target).catch(error=>getter.title=error.message);}else{}
 if(['input','textarea','slider'].includes(kind)){
  const setter=visualField(parent,'Setter method',node.setter,value=>{node.setter=value;viewEditorChanged();});visualMethodChoices(setter,viewEditorState.target).catch(error=>setter.title=error.message);
  visualField(parent,'Value type',node.type||'',value=>{if(value)node.type=value;else delete node.type;viewEditorChanged();},[{value:'',label:'Infer from getter'},'number','string','bool','nil','objid','character']);
 }else{}
 if(kind==='slider')for(const [key,fallback]of [['min',0],['max',100],['step',1]])visualField(parent,key==='min'?'Minimum':key==='max'?'Maximum':'Step',node[key]??fallback,value=>{if(!value.trim()||!Number.isFinite(Number(value)))throw Error('Enter a finite number.');else{}node[key]=Number(value);viewEditorChanged();});else{}
 if(kind==='action'){
  const method=visualField(parent,'Action method',node.method,value=>{node.method=value;viewEditorChanged();});visualMethodChoices(method,viewEditorState.target).catch(error=>method.title=error.message);
  visualField(parent,'Refresh after action',String(!!node.refresh),value=>{node.refresh=value==='true';viewEditorChanged();},[{value:'false',label:'No'},{value:'true',label:'Yes'}]);
 }else{}
 if(!['row','column'].includes(kind))visualArguments(parent,node,viewEditorChanged);else{}
 if(kind==='table')renderViewColumns(parent,node);else{}
}
function renderViewColumns(parent,node){
 const section=toolElement('section','');parent.append(section);section.append(toolElement('h4','Table columns'));const list=toolElement('div','');section.append(list);
 function render(){list.replaceChildren();(node.columns||[]).forEach((column,index)=>{const row=toolElement('div','');visualField(row,'Column '+(index+1)+' key',column.key,value=>{column.key=value;viewEditorChanged();});visualField(row,'Column '+(index+1)+' label',column.label,value=>{column.label=value;viewEditorChanged();});row.append(toolButton('Remove column '+(index+1),()=>{node.columns.splice(index,1);render();viewEditorChanged();}));list.append(row);});}
 section.append(toolButton('Add column',()=>{node.columns=node.columns||[];node.columns.push({key:'field',label:'Field'});render();viewEditorChanged();}),toolButton('Infer columns from data',()=>{delete node.columns;render();viewEditorChanged();}));render();
}
function newViewControl(kind){if(['row','column'].includes(kind))return {kind,children:[]};else if(kind==='action')return {kind,text:'Action',method:'value',args:[]};else if(['input','slider','textarea'].includes(kind))return {kind,label:'Value',getter:'value',setter:'',type:kind==='textarea'?'string':'number'};else return {kind,text:kind==='text'?'Text':'',getter:kind==='text'?'':'value'};}
function addViewControl(){
 if(!visualValid(document.getElementById('view-designer')))return;else{}
 const kind=document.getElementById('view-control-kind').value,selected=visualNodeAt(viewEditorRoot(),viewEditorState.path);
 if(['row','column'].includes(selected.kind||'column')){if(viewEditorState.path.length>=16){document.getElementById('view-designer-status').textContent='View nesting exceeds 16 levels.';return;}else{}selected.children=selected.children||[];selected.children.push(newViewControl(kind));viewEditorState.path=[...viewEditorState.path,selected.children.length-1];}
 else if(viewEditorState.path.length){const parentPath=viewEditorState.path.slice(0,-1),index=viewEditorState.path.at(-1),parent=visualNodeAt(viewEditorRoot(),parentPath);parent.children.splice(index+1,0,newViewControl(kind));viewEditorState.path=[...parentPath,index+1];}
 else{const root=visualCopy(selected),wrapped={kind:'column',children:[root,newViewControl(kind)]};if(viewEditorState.definition.body)viewEditorState.definition.body=wrapped;else viewEditorState.definition={id:selected.id,title:selected.title,body:wrapped};viewEditorState.path=[1];}
 viewEditorChanged();renderViewDesigner();
}
function changeViewControl(action){
 if(!visualValid(document.getElementById('view-designer'))||!viewEditorState.path.length)return;else{}
 const path=viewEditorState.path,parentPath=path.slice(0,-1),index=path.at(-1),parent=visualNodeAt(viewEditorRoot(),parentPath),node=parent.children[index];
 if(action==='remove'){parent.children.splice(index,1);viewEditorState.path=parentPath;}
 else if(action==='copy'){parent.children.splice(index+1,0,visualCopy(node));viewEditorState.path=[...parentPath,index+1];}
 else if(action==='out'){if(!parentPath.length)return;else{}const grandPath=parentPath.slice(0,-1),parentIndex=parentPath.at(-1),grand=visualNodeAt(viewEditorRoot(),grandPath);parent.children.splice(index,1);grand.children.splice(parentIndex+1,0,node);viewEditorState.path=[...grandPath,parentIndex+1];}
 else if(action==='in'){const previous=parent.children[index-1];if(!previous||!['row','column'].includes(previous.kind||'column')){document.getElementById('view-designer-status').textContent='Move a row or column directly above this control first.';return;}else{}previous.children=previous.children||[];previous.children.push(node);parent.children.splice(index,1);viewEditorState.path=[...parentPath,index-1,previous.children.length-1];}
 else{const next=index+(action==='up'?-1:1);if(next<0||next>=parent.children.length)return;else{}[parent.children[index],parent.children[next]]=[parent.children[next],parent.children[index]];viewEditorState.path=[...parentPath,next];}
 viewEditorChanged();renderViewDesigner();
}
async function previewViewDesigner(){
 const parent=document.getElementById('view-design-preview');parent.replaceChildren();if(viewEditorState.raw)return;else{}
 try{visualValidateView(viewEditorState.definition);const preview=toolElement('div','');parent.append(preview);
  await renderViewNode(viewEditorRoot(),viewEditorState.target,preview,inspectionVersion,0,(element,node,path)=>{
   element.dataset.controlPath=JSON.stringify(path);element.classList.add('visual-preview-control');element.tabIndex=0;element.setAttribute('aria-label','Select preview '+(node.label||node.text||node.getter||node.kind||'column'));
   if(JSON.stringify(path)===JSON.stringify(viewEditorState.path))element.classList.add('visual-selected');else{}
   element.addEventListener('click',event=>{if(event.target.closest('[data-control-path]')!==element)return;else{}event.preventDefault();event.stopImmediatePropagation();selectViewControl(path);},true);
   element.addEventListener('keydown',event=>{if(event.target.closest('[data-control-path]')===element&&['Enter',' '].includes(event.key)){event.preventDefault();event.stopImmediatePropagation();selectViewControl(path);}else{}},true);
  });
 }catch(error){parent.textContent=error.message;}
}
function toggleViewDesignerJSON(){
 try{if(viewEditorState.raw){viewEditorState.definition=visualValidateView(JSON.parse(document.getElementById('view-design-json').value));viewEditorRawDrafts.delete(viewEditorState.objectId);viewEditorState.path=[];viewEditorState.raw=false;renderViewDesigner();viewEditorChanged();}else{if(!visualValid(document.getElementById('view-designer')))return;else{}document.getElementById('view-design-json').value=JSON.stringify(viewEditorState.definition,null,2);viewEditorState.raw=true;document.getElementById('view-design-visual').hidden=true;document.getElementById('view-design-json').hidden=false;document.getElementById('view-design-mode').textContent='Visual editor';}}catch(error){document.getElementById('view-designer-status').textContent=error.message;}
}
async function saveViewDesigner(){
 try{if(!visualValid(document.getElementById('view-designer')))return;else{}const definition=visualValidateView(viewEditorState.raw?JSON.parse(document.getElementById('view-design-json').value):viewEditorState.definition);
  const response=await performInteraction({kind:'call',objectId:viewEditorState.objectId,method:'definition:',args:[{kind:'string',str:JSON.stringify(definition)}]});if(response.error)throw Error(response.error);else{}
  viewEditorState.definition=visualCopy(definition);if(viewEditorState.raw)viewEditorState.path=[];else{}viewEditorDrafts.set(viewEditorState.objectId,visualCopy(definition));viewEditorRawDrafts.delete(viewEditorState.objectId);document.getElementById('view-designer-status').textContent='Saved';await showInspect(viewEditorState.target,true);renderViewDesigner();
 }catch(error){document.getElementById('view-designer-status').textContent=error.message;}
}
async function reloadViewDesigner(){try{const saved=await inspectorCall(viewEditorState.objectId,'definition',[]);viewEditorState.definition=JSON.parse(saved.str);viewEditorState.path=[];viewEditorState.raw=false;viewEditorDrafts.delete(viewEditorState.objectId);viewEditorRawDrafts.delete(viewEditorState.objectId);renderViewDesigner();document.getElementById('view-designer-status').textContent='Reloaded';}catch(error){document.getElementById('view-designer-status').textContent=error.message;}}
`
