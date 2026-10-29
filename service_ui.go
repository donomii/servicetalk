package main

const serviceCSS = `
.service-columns{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:24px}.service-columns>section{min-width:0}.service-columns h4{font-size:15px;margin:20px 0 10px}#service-source{width:100%;min-height:230px;font:13px/1.6 var(--mono);padding:12px;resize:vertical}#service-generations{margin-top:14px}.service-generation{padding:16px;border:1px solid var(--line);border-radius:8px;margin:10px 0}.service-generation[data-state=active]{border-left:4px solid #4a6c5a}.service-generation>strong{font-size:14px;overflow-wrap:anywhere}.service-generation .result-actions{margin-bottom:0}.service-version{font:11px var(--mono);color:var(--muted);overflow-wrap:anywhere}#service-force{padding:14px;border:1px solid var(--accent);border-radius:6px;margin-top:12px}#service-check .marg{display:flex;flex-wrap:wrap}#service-check .marg input{min-width:100px;flex:1}.service-note{font-size:12px;color:var(--muted);margin:8px 0}.service-routing{padding:12px 16px;background:#edf2e8;border-radius:6px;margin:14px 0}@media(max-width:750px){.service-columns{grid-template-columns:minmax(0,1fr);gap:8px}}
`

const serviceHTML = `
<section id="service-panel" class="runtime-panel" hidden>
 <div class="runtime-toolbar"><strong>Services</strong><button class="btn" onclick="refreshServices()">Refresh</button><button class="btn" onclick="closeServices()">Close</button></div>
 <p class="session-muted">Prepare a version, check its behaviour, then send new calls to it.</p>
 <div id="service-message" class="runtime-status" role="status"></div>
 <div class="session-form"><label class="grow">Service<select id="service-select" onchange="selectService(this.value)"></select></label><label>New service name<input id="service-name" placeholder="e.g. formatter"></label><button class="btn" onclick="createService()">Create service</button></div>
 <div id="service-content" hidden><div id="service-routing" class="service-routing"></div>
 <div class="service-columns"><section><h4>1. Prepare an update</h4><div class="session-form"><label class="grow">Class<input id="service-class" list="service-class-choices" placeholder="Class name"></label><button class="btn" onclick="prepareServiceClass()">Build from files</button><button class="btn" onclick="loadServiceSource()">Load definition</button></div><datalist id="service-class-choices"></datalist>
 <details id="service-source-details"><summary>Edit a native definition</summary><textarea id="service-source" aria-label="Candidate service definition" spellcheck="false" placeholder="class Greeting [ version [ ^ 1 ] greet [ ^ 'hello' ] ]"></textarea><button class="btn" onclick="prepareServiceSource()">Prepare this definition</button><p class="service-note">This creates a candidate from the draft. Use Source / Git history to save changes to the project.</p></details>
 <h4>2. Check the candidate</h4><form id="service-check" onsubmit="checkServiceCandidate(event)"><div class="session-form"><label class="grow">Candidate<select id="service-candidate" onchange="selectServiceCandidate(this.value)"></select></label><label class="grow">Method<input id="service-check-method" list="service-method-choices" placeholder="e.g. version" oninput="serviceCheckArguments()"></label></div><datalist id="service-method-choices"></datalist><div id="service-check-arguments"></div><div id="service-check-expected"></div><button class="btn btn-run" id="service-check-run" disabled>Run check</button></form><p class="service-note">Choose a method and the result you expect. A passing check enables promotion.</p>
 </section><section><h4>3. Promote and drain</h4><div id="service-generations"></div><div id="service-force" hidden><p id="service-force-description"></p><div class="result-actions"><button class="btn btn-run" id="service-force-confirm">Stop this generation</button><button class="btn" onclick="document.getElementById('service-force').hidden=true">Cancel</button></div></div><div id="service-result"></div></section></div></div>
</section>
`

const serviceJS = `
let serviceID='',serviceStatus=null,serviceSignatures={},serviceRefreshBusy=false;
function serviceMessage(text){document.getElementById('service-message').textContent=text;}
async function serviceSend(method,args=[]){if(!serviceID)throw Error('Choose or create a service.');else{}const result=await performInteraction({kind:'call',objectId:serviceID,method,args});if(result.error)throw Error(result.error);else{return result;}}
async function openServices(){closeSession();closeShellPanel();document.getElementById('service-panel').hidden=false;await refreshServices();}
function closeServices(){document.getElementById('service-panel').hidden=true;}
async function refreshServices(){
 if(serviceRefreshBusy)return;else{}serviceRefreshBusy=true;
 try{const registry=await fetch('/registry').then(response=>response.json()),select=document.getElementById('service-select');select.replaceChildren();
  for(const entry of registry.registry||[]){if(entry.className!=='Service')continue;else{}let label=entry.id;try{const name=await inspectorCall(entry.id,'name',[]);if(name.str)label=name.str;else{}}catch(error){}const option=toolElement('option',label+' · '+entry.id.slice(0,8));option.value=entry.id;select.append(option);}
  if([...select.options].some(option=>option.value===serviceID))select.value=serviceID;else serviceID=select.value;
  document.getElementById('service-content').hidden=!serviceID;if(serviceID)await readServiceStatus();else serviceMessage('Create a named service to prepare its first version.');
  const navigation=await fetch('/navigation').then(response=>response.json()),choices=document.getElementById('service-class-choices');choices.replaceChildren();for(const name of [...new Set((navigation.methods||[]).map(item=>item.class))].sort()){const option=toolElement('option',name);option.value=name;choices.append(option);}
 }catch(error){serviceMessage(error.message);}finally{serviceRefreshBusy=false;}
}
async function selectService(id){serviceID=id;document.getElementById('service-force').hidden=true;await readServiceStatus();}
async function readServiceStatus(){
 if(!serviceID)return;else{}const id=serviceID;
 try{const value=await inspectorCall(id,'status',[]);if(id!==serviceID)return;else{}serviceStatus=JSON.parse(value.str);renderServiceStatus();}catch(error){serviceMessage(error.message);}
}
async function createService(){
 const name=document.getElementById('service-name').value.trim();if(!name){serviceMessage('Enter a service name.');return;}else{}
 try{const created=await performInteraction({kind:'eval',code:'Service new'});if(created.error)throw Error(created.error);else{}serviceID=created.result.objid;await serviceSend('name:',[{kind:'string',str:name}]);await refreshServices();serviceMessage('Created '+name+'. Prepare its first version.');renderResult(document.getElementById('service-result'),created);}catch(error){serviceMessage(error.message);}
}
async function prepareServiceClass(){const name=document.getElementById('service-class').value.trim();if(!name){serviceMessage('Enter a class name.');return;}else{}await prepareService('prepareClass:',name);}
async function prepareServiceSource(){const source=document.getElementById('service-source').value;if(!source.trim()){serviceMessage('Enter a definition.');return;}else{}await prepareService('prepareSource:',source);}
async function prepareService(method,source){
 serviceMessage('Preparing candidate…');try{const result=await serviceSend(method,[{kind:'string',str:source}]);await readServiceStatus();document.getElementById('service-candidate').value=result.result.str;await selectServiceCandidate(result.result.str);serviceMessage('Candidate prepared. Check its behaviour before promotion.');}catch(error){serviceMessage(error.message);await readServiceStatus();}
}
async function loadServiceSource(){
 const name=document.getElementById('service-class').value.trim();try{const source=await sourceAPI({action:'read',class:name});if(source.readOnly)throw Error('Use Build from files for this class.');else{}document.getElementById('service-source').value=source.source;document.getElementById('service-source-details').open=true;serviceMessage('Definition loaded into the candidate editor.');}catch(error){serviceMessage(error.message);}
}
async function prepareSourceServiceUpdate(){if(!sourceState)return;else{}const draft=document.getElementById('source-editor').value,className=sourceState.class;closeSource();await openServices();document.getElementById('service-class').value=className;document.getElementById('service-source').value=draft;document.getElementById('service-source-details').open=true;serviceMessage('Draft copied. Choose a service and prepare this definition.');}
function serviceCheckArguments(){
 const method=document.getElementById('service-check-method').value.trim(),names=serviceSignatures[method]||Array.from({length:(method.match(/:/g)||[]).length||(/^[^\w]+$/.test(method)?1:0)},(_,index)=>'Argument '+(index+1)),container=document.getElementById('service-check-arguments');container.replaceChildren();for(const name of names)container.append(valueEditor(name,{kind:'string',str:''}));
}
async function selectServiceCandidate(id){
 const generation=serviceStatus?.generations?.find(item=>item.id===id);serviceSignatures={};document.getElementById('service-method-choices').replaceChildren();if(!generation?.target)return;else{}
 try{const inspection=await fetch('/relationships?id='+encodeURIComponent(generation.target)).then(response=>response.json());if(document.getElementById('service-candidate').value!==id)return;else{}serviceSignatures=inspection.methodSignatures||{};for(const name of (inspection.methods||[]).sort()){const option=toolElement('option',name);option.value=name;document.getElementById('service-method-choices').append(option);}serviceCheckArguments();}catch(error){serviceMessage(error.message);}
}
async function checkServiceCandidate(event){
 event.preventDefault();event.submitter.disabled=true;
 try{const generation=document.getElementById('service-candidate').value,selector=document.getElementById('service-check-method').value.trim();if(!generation||!selector)throw Error('Choose a candidate and a method to check.');else{}const args=[...document.querySelectorAll('#service-check-arguments .marg')].map(argumentValue),expected=argumentValue(document.querySelector('#service-check-expected .marg'));
  const list=await performInteraction({kind:'eval',code:'{'+args.map(valueExpression).join('. ')+'}'});if(list.error)throw Error(list.error);else{}
  await serviceSend('check:selector:arguments:expect:',[{kind:'string',str:generation},{kind:'string',str:selector},list.result,expected]);serviceMessage('Check passed. This candidate is ready to promote.');await readServiceStatus();
 }catch(error){serviceMessage(error.message);await readServiceStatus();}finally{event.submitter.disabled=false;}
}
async function changeServiceGeneration(method,generation){try{await serviceSend(method,[{kind:'string',str:generation.id}]);document.getElementById('service-force').hidden=true;await readServiceStatus();serviceMessage(method==='promote:'?'New calls now use '+generation.id+'.':method==='forceRetire:'?'Generation stopped.':'Generation updated.');}catch(error){serviceMessage(error.message);await readServiceStatus();}}
function confirmServiceForce(generation){document.getElementById('service-force').hidden=false;document.getElementById('service-force-description').textContent='Stop '+generation.id+' and its running calls? Any clients holding its old object reference will receive errors.';document.getElementById('service-force-confirm').onclick=()=>changeServiceGeneration('forceRetire:',generation);}
function serviceObjectLink(id,label){const link=objectLink(id);link.textContent=label;link.title=id;link.onclick=()=>{closeServices();showInspect(id);};return link;}
function renderServiceStatus(){
 const routing=document.getElementById('service-routing');routing.replaceChildren();routing.append(document.createTextNode(serviceStatus.current?'New calls → '+serviceStatus.current+' · ':'No active version yet. '),serviceObjectLink(serviceID,'Inspect service'));
 const candidates=document.getElementById('service-candidate'),previous=candidates.value;candidates.replaceChildren();for(const generation of serviceStatus.generations||[]){if(!['candidate','ready','failed'].includes(generation.state))continue;else{}const option=toolElement('option',generation.id+' · '+generation.className);option.value=generation.id;candidates.append(option);}if([...candidates.options].some(item=>item.value===previous))candidates.value=previous;else if(candidates.value)selectServiceCandidate(candidates.value);else{}
 document.getElementById('service-check-run').disabled=!candidates.options.length;candidates.disabled=!candidates.options.length;
 const list=document.getElementById('service-generations'),focus=document.activeElement?.dataset.generationKey;list.replaceChildren();
 for(const generation of [...(serviceStatus.generations||[])].reverse()){const card=toolElement('div','','service-generation');card.dataset.state=generation.state;card.append(toolElement('strong',generation.id+' · '+generation.state),toolElement('div',generation.className+' · '+generation.inFlight+' running'+(generation.uncertain?' · completion uncertain':''),'service-note'),toolElement('div',generation.version,'service-version'));if(generation.error)card.append(toolElement('p',generation.error,'ev-err'));else{}
  const actions=toolElement('div','','result-actions');if(generation.target&&['candidate','ready','failed','active','draining','retired'].includes(generation.state)){const link=serviceObjectLink(generation.target,'Inspect version');actions.append(link);}else{}
  const add=(label,method)=>{const button=toolButton(label,()=>changeServiceGeneration(method,generation));button.dataset.generationKey=generation.id+method;actions.append(button);};if(generation.state==='ready')add('Promote','promote:');else{}if(['candidate','ready','failed'].includes(generation.state))add('Discard','discard:');else{}if(generation.state==='draining')add('Retire when idle','retire:');else{}if(generation.target&&['candidate','ready','failed','draining','retired'].includes(generation.state)){const force=toolButton('Force stop…',()=>confirmServiceForce(generation));force.dataset.generationKey=generation.id+'force';actions.append(force);}else{}card.append(actions);list.append(card);
 }
 if(!list.children.length)list.append(toolElement('p','Prepared versions will appear here.','service-note'));else{}if(focus)[...list.querySelectorAll('button')].find(item=>item.dataset.generationKey===focus)?.focus({preventScroll:true});else{}
}
document.getElementById('service-check-expected').append(valueEditor('Expected result',{kind:'number',num:1}));
document.addEventListener('keydown',event=>{if(event.key==='Escape')closeServices();else{}});
setInterval(()=>{if(!document.getElementById('service-panel').hidden&&!serviceRefreshBusy)readServiceStatus();else{}},2000);
`
