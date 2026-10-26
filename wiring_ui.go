package main

const wiringJS = `
let wiringState=null,wiringSelected='',wiringConnection='',wiringGeneration=0,wiringDirty=false,wiringView=[0,0,1000,650];
let wiringRegistry=[],wiringSignatures={};
function closeWiring(){document.getElementById('wiring-panel').hidden=true;wiringGeneration++;}
async function openWiring(id='') {
 const generation=++wiringGeneration;document.getElementById('wiring-panel').hidden=false;document.getElementById('wiring-status').textContent='Loading graph…';
 try {
  const [graph,registry]=await Promise.all([fetch('/wiring'+(id?'?id='+encodeURIComponent(id):'')).then(r=>r.json()),fetch('/registry').then(r=>r.json())]);
  if(generation!==wiringGeneration)return;else{}if(graph.error)throw Error(graph.error);else{}
  wiringState=graph;wiringRegistry=(registry.registry||[]).sort((a,b)=>a.className.localeCompare(b.className)||a.id.localeCompare(b.id));wiringSelected='';wiringDirty=false;
  const select=document.getElementById('wiring-object');select.replaceChildren();
  for(const object of wiringRegistry){const option=toolElement('option',object.className+' · '+object.id);option.value=object.id;select.append(option);}
  document.getElementById('wiring-owner').textContent='Wiring object '+graph.objectId;
  document.getElementById('wiring-inspect').onclick=()=>{closeWiring();showInspect(graph.objectId);};
  document.getElementById('wiring-detail').replaceChildren();fitWiring();renderWiring();showWiringStatus();
 }catch(error){if(generation===wiringGeneration)document.getElementById('wiring-status').textContent=error.message;else{}}
}
function wiringNode(id){return wiringState?.definition.nodes.find(node=>node.id===id);}
function wiringChanged(){wiringDirty=true;renderWiring();showWiringStatus();}
function showWiringStatus(){document.getElementById('wiring-status').textContent=(wiringDirty?'Unsaved graph · ':'')+(wiringState?.run?.error||wiringState?.run?.state||'Connect outputs to receiver or argument sockets.');}
async function addWiringNode() {
 if(!wiringState)return;else{}const generation=wiringGeneration;
 try {
  const target=document.getElementById('wiring-object').value;const response=await fetch('/relationships?id='+encodeURIComponent(target));const inspected=await response.json();
  if(generation!==wiringGeneration)return;else{}
  const methods=(inspected.methods||[]).sort();const method=methods.includes('value')?'value':methods.find(name=>!name.includes(':'))||methods[0];if(!method)throw Error('Receiver has no listed methods.');else{}
  const names=(inspected.methodSignatures||{})[method]||[];
  const node={id:'node_'+Date.now().toString(36)+'_'+Math.random().toString(36).slice(2,6),label:inspected.className,target:{value:{kind:'objid',objid:target}},method,args:names.map(()=>({value:{kind:'nil'}})),x:60+(wiringState.definition.nodes.length%3)*280,y:60+Math.floor(wiringState.definition.nodes.length/3)*200};
  wiringState.definition.nodes.push(node);wiringChanged();await selectWiringNode(node.id);
 }catch(error){document.getElementById('wiring-status').textContent=error.message;}
}
function wireInputEditor(label,input) {
 const row=valueEditor(label,input.value||{kind:'string',str:input.from});const type=row.querySelector('select'),field=row.querySelector('input');
 const option=toolElement('option','Node result');option.value='connection';type.append(option);
 if(input.from){type.value='connection';field.value=input.from;}else{}
 type.onchange=()=>{field.disabled=type.value==='nil';if(type.value==='connection')field.setAttribute('list','wiring-node-options');else configureResultInput(type,field);};type.onchange();return row;
}
function wireEditorInput(row){return row.querySelector('select').value==='connection'?{from:row.querySelector('input').value.trim()}:{value:argumentValue(row)};}
async function selectWiringNode(id) {
 wiringSelected=id;const node=wiringNode(id),generation=wiringGeneration;if(!node)return;else{}
 renderWiring();const detail=document.getElementById('wiring-detail');detail.replaceChildren(toolElement('h4',node.label||node.id));
 const target=wireInputEditor('Receiver',node.target);target.id='wire-edit-target';detail.append(target);
 const method=document.createElement('input');method.value=node.method;method.id='wire-edit-method';method.setAttribute('aria-label','Node method');method.setAttribute('list','wiring-method-options');detail.append(toolElement('label','Method'),method);
 const args=document.createElement('div');args.id='wire-edit-args';detail.append(args);
 const choices=document.getElementById('wiring-method-options');choices.replaceChildren();wiringSignatures={};
 if(node.target.value?.kind==='objid') {
  const response=await fetch('/relationships?id='+encodeURIComponent(node.target.value.objid));const info=await response.json();if(generation!==wiringGeneration||wiringSelected!==id)return;else{}
  wiringSignatures=info.methodSignatures||{};for(const name of (info.methods||[]).sort()){const option=document.createElement('option');option.value=name;choices.append(option);}
 }else{}
 function argumentsForMethod(){const count=(wiringSignatures[method.value]||[]).length||(method.value.match(/:/g)||[]).length||(/^[^a-zA-Z0-9_]+$/.test(method.value)?1:0);args.replaceChildren();for(let index=0;index<count;index++)args.append(wireInputEditor((wiringSignatures[method.value]||[])[index]||'Argument '+(index+1),node.args[index]||{value:{kind:'nil'}}));}
 method.onchange=argumentsForMethod;argumentsForMethod();
 detail.append(toolButton('Apply node',()=>{try{node.target=wireEditorInput(target);node.method=method.value.trim();node.args=[...args.children].map(wireEditorInput);wiringChanged();}catch(error){document.getElementById('wiring-status').textContent=error.message;}}));
 detail.append(toolButton('Remove node',()=>{wiringState.definition.nodes=wiringState.definition.nodes.filter(item=>item.id!==id);for(const item of wiringState.definition.nodes)for(const input of [item.target,...item.args])if(input.from===id){delete input.from;input.value={kind:'nil'};}else{}wiringSelected='';detail.replaceChildren();wiringChanged();}));
 const result=wiringState.run?.results?.[id];if(result){const output=toolElement('div','Result: '+fmtVal(result));if(result.kind==='objid')output.append(objectLink(result.objid));else{}detail.append(output);}else{}
}
function fitWiring(){const nodes=wiringState?.definition.nodes||[];if(!nodes.length){wiringView=[0,0,1000,650];return;}else{}const xs=nodes.map(n=>n.x||60),ys=nodes.map(n=>n.y||60);const left=Math.min(...xs)-50,top=Math.min(...ys)-50;wiringView=[left,top,Math.max(700,Math.max(...xs)-left+280),Math.max(400,Math.max(...ys)-top+220)];}
function wireSVG(tag,attributes,text=''){const node=document.createElementNS('http://www.w3.org/2000/svg',tag);for(const [key,value]of Object.entries(attributes))node.setAttribute(key,String(value));node.textContent=text;return node;}
function wireSocket(node,index,x,y,label,group) {
 const port=wireSVG('circle',{cx:x,cy:y,r:7,fill:'#fff',stroke:'#447a9c','stroke-width':2,tabindex:0,role:'button','aria-label':label+' of '+node.id});
 const connect=()=>{if(wiringConnection&&wiringConnection!==node.id){const input={from:wiringConnection};if(index<0)node.target=input;else node.args[index]=input;wiringConnection='';wiringChanged();selectWiringNode(node.id);}else{document.getElementById('wiring-status').textContent='Choose an output socket first.';}};
 port.onclick=event=>{event.stopPropagation();connect();};port.onkeydown=event=>{if(event.key==='Enter')connect();else{}};group.append(port,wireSVG('text',{x:x+12,y:y+4,'font-size':12},label));
}
function renderWiring() {
 if(!wiringState)return;else{}const svg=document.getElementById('wiring-graph');svg.replaceChildren();svg.setAttribute('viewBox',wiringView.join(' '));
 const choices=document.getElementById('wiring-node-options');choices.replaceChildren();for(const node of wiringState.definition.nodes){const option=toolElement('option',node.label+' '+node.method);option.value=node.id;choices.append(option);}
 for(const node of wiringState.definition.nodes)for(const [index,input]of [node.target,...node.args].entries())if(input.from){const source=wiringNode(input.from);if(!source)continue;else{}const x1=(source.x||60)+210,y1=(source.y||60)+24,x2=node.x||60,y2=(node.y||60)+48+index*22;svg.append(wireSVG('path',{d:'M'+x1+','+y1+' C'+(x1+70)+','+y1+' '+(x2-70)+','+y2+' '+x2+','+y2,stroke:'#447a9c','stroke-width':2,fill:'none'}));}else{}
 for(const node of wiringState.definition.nodes)renderWireNode(svg,node);
}
function renderWireNode(svg,node) {
 const x=node.x||60,y=node.y||60,group=wireSVG('g',{}),result=wiringState.run?.results?.[node.id];
 const failed=wiringState.run?.failedNode===node.id;const color=failed?'#c0392b':result?'#3c8c66':'#788894';
 group.append(wireSVG('rect',{x,y,width:210,height:80+node.args.length*22,rx:6,fill:node.id===wiringSelected?'#edf5fb':'white',stroke:color,'stroke-width':2}));
 group.append(wireSVG('text',{x:x+12,y:y+19,'font-size':12,'font-weight':'bold'},(node.label||node.id).slice(0,26)),wireSVG('text',{x:x+12,y:y+36,'font-size':12},node.method));
 wireSocket(node,-1,x,y+48,'receiver',group);node.args.forEach((_,index)=>wireSocket(node,index,x,y+70+index*22,'argument '+(index+1),group));
 const output=wireSVG('circle',{cx:x+210,cy:y+24,r:8,fill:color,tabindex:0,role:'button','aria-label':'Output of '+node.id});
 const choose=()=>{wiringConnection=node.id;document.getElementById('wiring-status').textContent='Connect '+node.method+' to a receiver or argument socket.';};output.onclick=event=>{event.stopPropagation();choose();};output.onkeydown=event=>{if(event.key==='Enter')choose();else{}};group.append(output);
 if(result)group.append(wireSVG('text',{x:x+12,y:y+70+node.args.length*22,'font-size':11,fill:color},fmtVal(result).slice(0,27)));else{}
 let drag=null,moved=false;const point=event=>{const p=svg.createSVGPoint();p.x=event.clientX;p.y=event.clientY;return p.matrixTransform(svg.getScreenCTM().inverse());};
 group.onpointerdown=event=>{if(event.target.tagName==='circle')return;else{}const p=point(event);drag={x:p.x,y:p.y,initialX:x,initialY:y};moved=false;group.setPointerCapture(event.pointerId);};
 group.onpointermove=event=>{if(!drag)return;else{}const p=point(event),dx=p.x-drag.x,dy=p.y-drag.y;moved=moved||Math.abs(dx)+Math.abs(dy)>3;group.setAttribute('transform','translate('+dx+','+dy+')');node.x=drag.initialX+dx;node.y=drag.initialY+dy;};
 group.onpointerup=()=>{if(!drag)return;else{}drag=null;if(moved)wiringChanged();else selectWiringNode(node.id);};svg.append(group);
}
async function saveWiring() {
 if(!wiringState)return false;else{}
 const response=await performInteraction({kind:'call',objectId:wiringState.objectId,method:'definition:',args:[{kind:'string',str:JSON.stringify(wiringState.definition)}]});
 if(response.error)throw Error(response.error);else{}wiringDirty=false;showWiringStatus();return true;
}
async function runWiring(resume=false) {
 if(!wiringState)return;else{}
 try {
  if(wiringDirty)await saveWiring();else{}document.getElementById('wiring-status').textContent=resume?'Resuming…':'Running…';
  const response=await performInteraction({kind:'call',objectId:wiringState.objectId,method:resume?'resume':'run',args:[]});
  const report=await inspectorCall(wiringState.objectId,'lastRun',[]);wiringState.run=JSON.parse(report.str);renderWiring();if(wiringSelected)await selectWiringNode(wiringSelected);else{}
  if(response.error)throw Error(response.error);else{}showWiringStatus();
 }catch(error){document.getElementById('wiring-status').textContent=error.message;}
}
document.getElementById('wiring-save').onclick=()=>saveWiring().catch(error=>document.getElementById('wiring-status').textContent=error.message);
`
