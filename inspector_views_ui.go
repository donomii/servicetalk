package main

const inspectorViewsJS = `const selectedViews=new Map();let liveViewTimer;
async function loadInspectorViews(id,inspection,version){
  clearTimeout(liveViewTimer);
  const container=document.getElementById('mviews');container.replaceChildren();
  if(!(inspection.methods||[]).includes('inspectorViews'))return;else{}
  try{
    const reply=await inspectorCall(id,'inspectorViews',[]);
    const definitions=JSON.parse(reply.str||'{}').views||[];
    if(!Array.isArray(definitions))throw Error('inspectorViews.views must be an array');else{}
    if(version!==inspectionVersion)return;else{}
    const tabs=document.createElement('div');tabs.className='view-tabs';container.appendChild(tabs);
    const body=document.createElement('div');body.className='view-body';container.appendChild(body);
    let selectionVersion=0;const tabButtons=new Map();
    async function select(definition){
      const choice=++selectionVersion;
      selectedViews.set(id,definition.id);for(const [key,button]of tabButtons)button.setAttribute('aria-pressed',String(key===definition.id));
      body.textContent='Loading…';
      try{
        const view=await uiPost('/view',{target:id,key:definition.id,definition});
        if(view.error)throw Error(view.error);else{}
        if(version!==inspectionVersion || choice!==selectionVersion)return;else{}
        tabButtons.get(definition.id).textContent=view.definition.title||definition.title||definition.id;
        async function render(){body.replaceChildren();
        const controls=document.createElement('div');controls.className='view-tools';
        const title=document.createElement('strong');title.textContent=view.definition.title||definition.title||definition.id;controls.appendChild(title);
        controls.appendChild(navButton('Inspect view object',()=>showInspect(view.objectId)));
        controls.appendChild(navButton('Edit layout',()=>editInspectorView(view,id,body,version)));
        body.appendChild(controls);
        await renderViewNode(view.definition.body||view.definition,id,body,version,0);
        }
        await render();let last=JSON.stringify(view.definition);
        async function watch(){
          if(version!==inspectionVersion||choice!==selectionVersion||!body.isConnected)return;else{}
          try{const value=await inspectorCall(view.objectId,'definition',[]);if(version!==inspectionVersion||choice!==selectionVersion)return;else{}
            const definition=JSON.parse(value.str),next=JSON.stringify(definition);view.definition=definition;receiveLiveControlView(view);
            if(next!==last&&!(body.contains(document.activeElement)&&document.activeElement.matches('input,textarea,select'))){last=next;tabButtons.get(selectedViews.get(id)).textContent=definition.title||selectedViews.get(id);await render();}else{}
          }catch(error){body.title=error.message;}finally{if(version===inspectionVersion&&choice===selectionVersion)liveViewTimer=setTimeout(watch,1000);else{}}
        }
        clearTimeout(liveViewTimer);liveViewTimer=setTimeout(watch,1000);
      }catch(error){if(version===inspectionVersion)body.textContent=error.message;else{}}
    }
    definitions.forEach(definition=>{if(!definition.id)throw Error('view requires id');else{const tab=navButton(definition.title||definition.id,()=>select(definition));tabButtons.set(definition.id,tab);tabs.appendChild(tab);}});
    const initial=definitions.find(view=>view.id===selectedViews.get(id))||definitions[0];
    if(initial)await select(initial);else{}
  }catch(error){if(version===inspectionVersion)container.textContent='Views: '+error.message;else{}}
}
function editInspectorView(view,id,container,version){
  openViewDesigner(view,id);
}
function viewValue(container,value){
  if(value && value.kind==='objid'){container.appendChild(objectLink(value.objid));}
  else{container.appendChild(document.createTextNode(value && value.kind?fmtVal(value):typeof value==='object'?JSON.stringify(value):String(value??'')));}
}
async function viewRead(node,id,preview=false){
  if(node.controlId&&!preview)return inspectorCall(node.controlId,'read',[]);else{}
  if(node.getter)return inspectorCall(id,node.getter,node.args||[]);
  else return {kind:'string',str:node.text||''};
}
async function viewAction(id,method,args,result,label=''){
  const response=await performInteraction({kind:'call',objectId:id,method,args,label});
  renderResult(document.getElementById('mview-result'),response);
  if(response.error)return false;else{return true;}
}
async function renderViewNode(node,id,parent,version,depth,decorate,path=[],inheritedReceiver=id){
  if(depth>16)throw Error('View nesting exceeds 16 levels');else{}
  const element=document.createElement('div');element.className='view-node view-'+(node.kind||'column');parent.appendChild(element);
  if(decorate)decorate(element,node,path);else{}
  const receiver=node.receiver||inheritedReceiver;
  if(node.label){const label=document.createElement('label');label.textContent=node.label;element.appendChild(label);}else{}
  try{
    switch(node.kind||'column'){
      case 'row':case 'column':
        for(const [index,child]of (node.children||[]).entries())await renderViewNode(child,id,element,version,depth+1,decorate,[...path,index],receiver);
        break;
      case 'text':case 'value':case 'code':{
        const value=await viewRead(node,receiver,!!decorate);const output=document.createElement(node.kind==='code'?'pre':'span');viewValue(output,value);element.appendChild(output);break;
      }
      case 'input':case 'slider':case 'textarea':{
        const value=await viewRead(node,receiver,!!decorate);const kind=node.type||value.kind;const row=document.createElement('div');row.className='marg';
        const type=document.createElement('select');type.hidden=true;const option=document.createElement('option');option.value=kind;type.appendChild(option);
        const input=document.createElement(node.kind==='textarea'?'textarea':'input');input.setAttribute('aria-label',node.label||node.setter||node.getter);
        input.value=value.kind==='string'?value.str||'':value.kind==='objid'?value.objid:fmtVal(value);
        if(node.kind==='slider'){input.type='range';input.min=node.min??0;input.max=node.max??100;input.step=node.step??1;}else{}
        const shown=document.createElement('span');shown.textContent=node.kind==='slider'?input.value:'';input.oninput=()=>{if(node.kind==='slider')shown.textContent=input.value;else{}};
        const status=document.createElement('div');
        const apply=navButton('Apply',async()=>{apply.disabled=true;try{
          let argument;
          if(node.kind==='textarea')argument={kind:'string',str:input.value};else argument=argumentValue(row);
          const ok=await viewAction(node.controlId||receiver,node.controlId?'write:':node.setter,[argument],status,node.label?'Set '+node.label:'');
          if(ok && version===inspectionVersion)await showInspect(id,true);else{}
        }catch(error){status.textContent=error.message;}finally{apply.disabled=false;}});
        input.onkeydown=event=>{if(event.key==='Enter' && node.kind!=='textarea'){event.preventDefault();apply.click();}else{}};
        row.append(type,input,shown);if(node.setter)row.appendChild(apply);else input.disabled=true;
        element.append(row,status);break;
      }
      case 'action':{
        const result=document.createElement('div');
        const button=navButton(node.text||node.method,async()=>{button.disabled=true;try{
          const ok=await viewAction(node.controlId||node.wiring||receiver,node.controlId?'invoke':node.wiring?'run':node.method,node.controlId||node.wiring?[]:node.args||[],result,node.text||node.method||'Action');
          if(ok && node.refresh && version===inspectionVersion)await showInspect(id,true);else{}
        }catch(error){result.textContent=error.message;}finally{button.disabled=false;}});
        element.append(button,result);break;
      }
      case 'table':case 'list':{
        const value=await viewRead(node,receiver,!!decorate);
        const data=value.kind==='string'?JSON.parse(value.str||'[]'):[];
        if(!Array.isArray(data))throw Error('Table/list getter must return a JSON array');else{}
        if(node.kind==='list'){data.forEach(item=>{const row=document.createElement('div');viewValue(row,item);element.appendChild(row);});}
        else{
          const scroll=document.createElement('div');scroll.className='view-table';const table=document.createElement('table');
          const columns=node.columns||Object.keys(data[0]||{}).map(key=>({key,label:key}));
          const head=document.createElement('tr');columns.forEach(column=>{const cell=document.createElement('th');cell.textContent=column.label||column.key;head.appendChild(cell);});table.appendChild(head);
          data.forEach(item=>{const row=document.createElement('tr');columns.forEach(column=>{const cell=document.createElement('td');viewValue(cell,item[column.key]);row.appendChild(cell);});table.appendChild(row);});scroll.appendChild(table);element.appendChild(scroll);
        }break;
      }
      default:throw Error('Unknown view control '+node.kind);
    }
  }catch(error){const message=document.createElement('div');message.className='view-error';message.textContent=error.message;element.appendChild(message);}
  if(version!==inspectionVersion)element.remove();else{}
}
`
