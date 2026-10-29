package main

const visualEditorJS = visualEditorCommonJS + workbenchEditorJS + viewEditorJS + controlEditorJS

const visualEditorCommonJS = `
const visualKinds=['column','row','text','value','code','input','slider','textarea','action','table','list'];
const visualActions={results:'Results',record:'Record',recording:'Recording',methods:'Browse methods',objects:'Object graph',trace:'Message trace',debugger:'Debugger',wiring:'Wiring',examples:'Examples',shell:'Shell',services:'Services',session:'Shared session',inspect:'Inspect an object',call:'Send a message'};
function visualCopy(value){return JSON.parse(JSON.stringify(value));}
function visualField(parent,label,value,change,options){
 const wrapper=toolElement('label',label),input=document.createElement(options?'select':'input');wrapper.className='visual-field';input.setAttribute('aria-label',label);
 if(options){for(const option of options){const item=toolElement('option',typeof option==='string'?option:option.label);item.value=typeof option==='string'?option:option.value;input.append(item);}}else{}
 let dirty=false;input.value=value??'';input.oninput=()=>{dirty=true;input.setCustomValidity('');};input.onchange=()=>{dirty=false;try{change(input.value);input.setCustomValidity('');}catch(error){input.setCustomValidity(error.message);input.reportValidity();}};input.onblur=()=>{if(dirty)input.onchange();else{}};wrapper.append(input);parent.append(wrapper);return input;
}
function visualValid(panel){return [...panel.querySelectorAll('input,textarea,select')].filter(input=>!input.closest('[hidden]')).every(input=>input.reportValidity());}
function visualArguments(parent,owner,change,names=[]){
 const section=toolElement('section','');section.className='visual-arguments';section.append(toolElement('h4','Arguments'));const rows=toolElement('div','');section.append(rows);parent.append(section);
 function render(){rows.replaceChildren();(owner.args||[]).forEach((value,index)=>{
  const row=valueEditor(names[index]||'Argument '+(index+1),value);const input=row.querySelector('input'),type=row.querySelector('select');
  const update=(report=true)=>{try{const next=argumentValue(row);input.setCustomValidity('');owner.args[index]=next;change();}catch(error){input.setCustomValidity(error.message);if(report)input.reportValidity();else{}}};
  const configure=type.onchange;type.onchange=()=>{if(configure)configure();else{}update();};input.onchange=()=>update();input.oninput=()=>update(false);
  row.append(toolButton('Remove argument '+(index+1),()=>{owner.args.splice(index,1);render();change();}));rows.append(row);
 });}
 section.append(toolButton('Add argument',()=>{owner.args=owner.args||[];owner.args.push({kind:'nil'});render();change();}));render();
}
async function visualMethodChoices(input,target){
 if(!target)return null;else{}
 const info=await fetch('/relationships?id='+encodeURIComponent(target)).then(r=>r.json());
 if(info.error)throw Error(info.error);else{}
 const choices=document.createElement('datalist');choices.id='visual-methods-'+Math.random().toString(36).slice(2);input.setAttribute('list',choices.id);
 for(const name of [...(info.methods||[])].sort()){const option=document.createElement('option');option.value=name;choices.append(option);}input.after(choices);return info;
}
function visualNodeAt(root,path){return path.reduce((node,index)=>node.children[index],root);}
function visualValidateView(definition){
 if(!definition||typeof definition!=='object'||Array.isArray(definition))throw Error('A view needs a definition object.');else{}
 function visit(node,depth){
  if(!node||typeof node!=='object'||Array.isArray(node)||!visualKinds.includes(node.kind||'column'))throw Error('Choose a supported control kind.');else{}
  if(depth>16)throw Error('View nesting exceeds 16 levels.');else{}
  if(node.args!==undefined&&!Array.isArray(node.args))throw Error('Arguments must be an array.');else{}
  if(node.children!==undefined&&!Array.isArray(node.children))throw Error('Children must be an array.');else{}
  if(node.columns!==undefined&&(!Array.isArray(node.columns)||node.columns.some(column=>!column||typeof column.key!=='string')))throw Error('Each table column needs a key.');else{}
  if(node.kind==='action'&&(node.action==='wiring'?!node.wiring:!node.wiring&&!node.method))throw Error('An action needs a method or a Wiring object.');else{}
  if(['slider'].includes(node.kind)&&(!(Number(node.step??1)>0)||Number(node.min??0)>Number(node.max??100)))throw Error('Slider step must be positive and minimum must not exceed maximum.');else{}
  for(const child of node.children||[])visit(child,depth+1);
 }
 visit(definition.body||definition,0);return definition;
}
`
