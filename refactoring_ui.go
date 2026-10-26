package main

const refactoringJS = `
function openRefactor(className,selector){
 document.getElementById('refactor-panel').hidden=false;document.getElementById('refactor-from').value=selector;document.getElementById('refactor-to').value='';
 const scope=document.getElementById('refactor-scope');scope.replaceChildren();const local=toolElement('option',className);local.value=className;const all=toolElement('option','All native classes');all.value='';scope.append(local,all);
 document.getElementById('refactor-results').replaceChildren();document.getElementById('refactor-to').focus();
}
function closeRefactor(){document.getElementById('refactor-panel').hidden=true;}
async function previewRefactor(){
 const container=document.getElementById('refactor-results');container.textContent='Reading selector locations…';
 try{const response=await uiPost('/refactor',{from:document.getElementById('refactor-from').value.trim(),to:document.getElementById('refactor-to').value.trim(),class:document.getElementById('refactor-scope').value});container.replaceChildren();
  for(const error of response.errors||[])container.append(toolElement('p',error));
  for(const file of response.files||[])renderRefactorFile(container,file);
  if(!(response.files||[]).length&&!(response.errors||[]).length)container.textContent='No matching native selectors.';else{}
 }catch(error){container.textContent=error.message;}
}
function renderRefactorFile(container,file){
 const card=toolElement('section','');card.className='refactor-file';card.append(toolElement('h4',file.class));
 for(const edit of file.edits)card.append(toolElement('div','Line '+edit.line+' · '+edit.kind+' · '+edit.before+' → '+edit.after));
 if(file.literals.length)card.append(toolElement('p','Literal references to review: lines '+[...new Set(file.literals)].join(', ')));else{}
 const status=toolElement('span','');
 if(file.edits.length){
  const source=toolElement('details','');source.append(toolElement('summary','Proposed source'),toolElement('pre',file.source));card.append(source);
  const save=toolButton('Save '+file.class,async()=>{save.disabled=true;try{const response=await uiPost('/source',{action:'save',class:file.class,digest:file.digest,source:file.source});if(response.error)throw Error(response.error);else{}status.textContent='Saved';}catch(error){status.textContent=error.message;save.disabled=false;}});card.append(save);
 }else{}
 card.append(toolButton('Source / Git history',()=>{closeRefactor();openSource(file.class);}),status);container.append(card);
}
`
