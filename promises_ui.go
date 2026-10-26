package main

const promisesJS = `
let promiseRefresh=null;
function renderPromise(id,snapshot,version) {
 clearTimeout(promiseRefresh);const panel=document.getElementById('mpromise');panel.replaceChildren();
 if(!snapshot)return;else{}
 panel.append(toolElement('h4','Promise · '+snapshot.state));
 const message=toolElement('div',snapshot.method+' → ');if(snapshot.target.kind==='objid')message.append(objectLink(snapshot.target.objid));else{}panel.append(message);
 if(snapshot.arguments.kind==='objid'){const args=toolElement('div','Arguments ');args.append(objectLink(snapshot.arguments.objid));panel.append(args);}else{}
 if(snapshot.error)panel.append(toolElement('div',snapshot.error));else{}
 if(snapshot.state==='fulfilled'){
  const result=toolElement('div','Result: ');if(snapshot.result.kind==='objid')result.append(objectLink(snapshot.result.objid));else result.append(document.createTextNode(fmtVal(snapshot.result)));panel.append(result);
  panel.append(toolButton('Use result',async()=>{const response=await performInteraction({kind:'call',objectId:id,method:'result',args:[]});renderResult(document.getElementById('mresult'),response);}));
 }else if(snapshot.state==='pending'){
  promiseRefresh=setTimeout(()=>{if(inspectedId===id&&inspectionVersion===version)showInspect(id,true);else{}},500);
 }else{}
}
async function sendObjectPromise() {
 const id=inspectedId,result=document.getElementById('mresult');const button=document.getElementById('mpromise-send');button.disabled=true;
 try {
  const method=document.getElementById('mselector').value.trim();if(!method)throw Error('Choose a method.');else{}
  const args=[...document.querySelectorAll('#margs .marg')].map(argumentValue);
  const response=await performInteraction({kind:'promise',objectId:id,method,args});if(response.error)throw Error(response.error);else{}
  if(inspectedId!==id)return;else{}await showInspect(response.result.objid);await refreshObjs();
 }catch(error){result.textContent=error.message;}finally{button.disabled=false;}
}
async function runWiringPromise() {
 if(!wiringState)return;else{}
 try {
  if(wiringDirty)await saveWiring();else{}
  const response=await performInteraction({kind:'promise',objectId:wiringState.objectId,method:'run',args:[]});
  if(response.error)throw Error(response.error);else{}closeWiring();await showInspect(response.result.objid);
 }catch(error){document.getElementById('wiring-status').textContent=error.message;}
}
`
