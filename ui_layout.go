package main

const workbenchLayoutJS = `
let objectEntries=[],inspectorSection='view',inspectorReturnFocus=null;
function renderObjectList(){
 const query=document.getElementById('object-filter').value.toLowerCase(),internal=document.getElementById('object-internals').checked;
 const entries=objectEntries.filter(item=>(internal||item.className==='__process__'||!item.className.startsWith('__'))&&(!query||(resultClassName(item.className)+' '+item.id).toLowerCase().includes(query))).sort((a,b)=>resultClassName(a.className).localeCompare(resultClassName(b.className))||a.id.localeCompare(b.id));
 document.getElementById('ocount').textContent=entries.length+' / '+objectEntries.length;
 const body=document.getElementById('otbody'),focused=body.contains(document.activeElement)?document.activeElement.dataset.objectId:null;body.replaceChildren();
 for(const item of entries){const row=document.createElement('tr');
  row.innerHTML='<td class="cn">'+esc(resultClassName(item.className))+'</td><td class="oid">'+esc(item.id)+'</td>';
  const cell=document.createElement('td'),button=document.createElement('button');button.className='object-open';button.dataset.objectId=item.id;button.textContent='Open';button.setAttribute('aria-label','Open '+resultClassName(item.className)+' '+item.id);button.onclick=event=>{event.stopPropagation();showInspect(item.id);};cell.append(button);row.append(cell);row.onclick=()=>showInspect(item.id);body.append(row);if(focused===item.id)button.focus({preventScroll:true});else{}
 }
 if(!entries.length){const row=document.createElement('tr'),cell=document.createElement('td');cell.colSpan=3;cell.textContent=query?'No matching objects':'No objects to show';row.append(cell);body.append(row);}else{}
}
function selectInspectorSection(name){
 inspectorSection=name;document.getElementById('mresult').hidden=['view','workspace'].includes(name);
 for(const section of ['view','methods','state','workspace']){document.getElementById('inspect-'+section).hidden=section!==name;document.getElementById('inspect-tab-'+section).setAttribute('aria-selected',String(section===name));document.getElementById('inspect-tab-'+section).tabIndex=section===name?0:-1;}
 document.getElementById('modal').scrollTop=0;
}
function prepareInspectorSections(hasView,preserve){
 document.getElementById('inspect-tab-view').hidden=!hasView;
 selectInspectorSection(preserve&&(inspectorSection!=='view'||hasView)?inspectorSection:hasView?'view':'methods');
}
function filterInspectorMethods(){const query=document.getElementById('method-filter').value.toLowerCase();for(const button of document.querySelectorAll('#mmths button'))button.hidden=!button.textContent.toLowerCase().includes(query);}
document.getElementById('modal').addEventListener('keydown',event=>{
 if(event.key!=='Tab')return;else{}
 const controls=[...document.getElementById('modal').querySelectorAll('button,input,select,textarea,summary,[tabindex="0"]')].filter(element=>!element.disabled&&element.tabIndex>=0&&element.getClientRects().length);
 if(!controls.length)return;else{}const first=controls[0],last=controls.at(-1);
 if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}else{}
});
document.getElementById('inspect-tabs').onkeydown=event=>{
 if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;else{}event.preventDefault();
 const tabs=[...document.querySelectorAll('#inspect-tabs button')].filter(button=>!button.hidden),index=tabs.indexOf(document.activeElement),next=event.key==='Home'?0:event.key==='End'?tabs.length-1:(index+(event.key==='ArrowRight'?1:-1)+tabs.length)% (tabs.length);
 tabs[next].click();tabs[next].focus();
};
setInterval(refreshObjs,2000);refreshObjs();
`
