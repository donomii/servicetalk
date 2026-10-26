package main

const navigationJS = `let navIndex = {methods:[],errors:[]};
let navMode = 'methods';
let navFrame = 0;
let navGeneration = 0;
let navSelected = '';
const navColors = {sends:'#bd6b27',implements:'#6477bd',reference:'#3c8995',owner:'#a15aa0',prototype:'#7952a8',root:'#4c9760',placement:'#999',waiting:'#bd6b27',executing:'#bd6b27'};
function navButton(label, action) {
  const button = document.createElement('button');
  button.className = 'btn'; button.textContent = label; button.onclick = action;
  return button;
}
async function openNavigation(selector = '', mode = 'methods') {
  document.getElementById('navigation').hidden = false;
  navMode = mode; navSelected = selector;
  document.getElementById('nav-search').value = selector;
  await refreshNavigation();
}
function closeNavigation() {
  document.getElementById('navigation').hidden = true;
  ++navGeneration; cancelAnimationFrame(navFrame);
}
async function refreshNavigation() {
  const generation = ++navGeneration;
  const detail = document.getElementById('nav-detail');
  detail.textContent = 'Loading…';
  try {
    if (navMode === 'objects') {
      await objectGraph(generation);
    } else {
      const response = await fetch('/navigation');
      if (!response.ok) throw Error(await response.text()); else { navIndex = await response.json(); }
      if (generation !== navGeneration) return; else { renderNavigation(); }
    }
  } catch (error) { if (generation === navGeneration) detail.textContent = error.message; else {} }
}
function renderNavigation() {
  const query = document.getElementById('nav-search').value.trim();
  navSelected = query;
  const detail = document.getElementById('nav-detail'); detail.replaceChildren();
  const list = document.createElement('div'); detail.appendChild(list);
  const matches = navIndex.methods.filter(m => !query || m.selector.includes(query) || m.class.includes(query));
  const exact = navIndex.methods.filter(m => m.selector === query);
  const senders = navIndex.methods.filter(m => m.sends.includes(query));
  function group(title, methods) {
    const heading = document.createElement('h4'); heading.textContent = title; list.appendChild(heading);
    methods.forEach(m => list.appendChild(navButton(m.class + ' » ' + m.selector, () => showMethodSource(m))));
    if (!methods.length) list.appendChild(document.createTextNode('None')); else {}
  }
  group(exact.length ? 'Implementors' : 'Methods', exact.length ? exact : matches);
  if (query) group('Senders', senders); else {}
  navIndex.errors.forEach(error => { const line = document.createElement('p'); line.textContent = error; list.appendChild(line); });
  const nodes = new Map(), edges = [];
  const add = (id,label,action) => { if (!nodes.has(id)) nodes.set(id,{id,label,action}); else {} };
  const methods = query ? [...new Set([...matches,...senders])] : matches;
  methods.forEach(m => {
    const id = m.class + ' » ' + m.selector;
    add(id,id,() => showMethodSource(m));
    const selector = 'selector:' + m.selector;
    add(selector,m.selector,() => openNavigation(m.selector));
    edges.push({from:id,to:selector,kind:'implements',label:'implements'});
    m.sends.forEach(send => {
      add('selector:' + send,send,() => openNavigation(send));
      edges.push({from:id,to:'selector:' + send,kind:'sends',label:'sends'});
    });
  });
  springGraph([...nodes.values()],edges);
}
function showMethodSource(method) {
  const detail = document.getElementById('nav-detail'); detail.replaceChildren();
  detail.appendChild(navButton('Back to results',renderNavigation));
  detail.appendChild(navButton('Implementors / senders',() => openNavigation(method.selector)));
  detail.appendChild(navButton('Edit source / Git history',() => openSource(method.class)));
  if(!method.foreign)detail.appendChild(navButton('Rename selector',()=>openRefactor(method.class,method.selector)));else{}
  const title = document.createElement('h4'); title.textContent = method.class + ' » ' + method.selector; detail.appendChild(title);
  const file = document.createElement('p'); file.textContent = method.file + (method.foreign ? ' — foreign declaration' : ' — class source'); detail.appendChild(file);
  method.sends.forEach(selector => detail.appendChild(navButton('Sends ' + selector,() => openNavigation(selector))));
  const pre = document.createElement('pre'); pre.tabIndex = 0;
  method.source.split('\n').forEach((text,i) => {
    const line = document.createElement('div'); line.textContent = String(i+1).padStart(4) + '  ' + text;
    if (i+1 === method.line) { line.className = 'source-current'; } else {}
    pre.appendChild(line);
  });
  detail.appendChild(pre);
  const current = pre.querySelector('.source-current');
  if (current) pre.scrollTop = current.offsetTop - pre.offsetTop - 40; else {}
}
async function objectGraph(generation) {
  const response = await fetch('/registry');
  if (!response.ok) throw Error(await response.text()); else {}
  const registry = (await response.json()).registry || [];
  const nodes = new Map(), edges = [], errors = [];
  const add = (id,label,action) => { if (!nodes.has(id)) nodes.set(id,{id,label,action}); else {} };
  registry.sort((a,b) => a.id.localeCompare(b.id)).forEach(entry => add(entry.id,entry.className + '\n' + entry.id,() => showInspect(entry.id)));
  let next = 0;
  async function worker() {
    while (next < registry.length && generation === navGeneration) {
      const entry = registry[next++];
      try {
        const response = await fetch('/relationships?id=' + encodeURIComponent(entry.id),{signal:AbortSignal.timeout(10000)});
        if (!response.ok) throw Error(await response.text()); else {}
        const item = await response.json(), runtime = item.runtime || {};
        function edge(target,kind,label) {
          add(target,target,() => showInspect(target)); edges.push({from:entry.id,to:target,kind,label});
        }
        const named = new Set();
        Object.entries(item.instanceVars || {}).forEach(([name,value]) => {
          if (value.kind === 'objid' && value.objid) { edge(value.objid,name === '(parent)' ? 'owner' : 'reference',name === '(parent)' ? 'lexical owner' : name); named.add(value.objid); } else {}
        });
        (runtime.references || []).forEach(id => { if (!named.has(id)) edge(id,'reference','reference'); else {} });
        if (runtime.prototype) edge(runtime.prototype,'prototype','cloned from'); else {}
        (runtime.calls || []).forEach(call => { if (call.target && call.target !== entry.id) edge(call.target,call.state,call.state + ' ' + call.method); else {} });
        if ((item.retention || {}).root) { add('roots','GC roots',null); edges.push({from:'roots',to:entry.id,kind:'root',label:'root'}); } else {}
        if (runtime.host) {
          const host = 'host:' + runtime.host + ':' + (runtime.workerId || 'coordinator');
          add(host,runtime.host + '\n' + (runtime.workerId || 'coordinator host'),null);
          edges.push({from:entry.id,to:host,kind:'placement',label:'hosted on'});
        } else {}
      } catch(error) { errors.push(entry.id + ': ' + error.message); }
    }
  }
  await Promise.all(Array.from({length:6},worker));
  if (generation !== navGeneration) return; else {}
  const detail = document.getElementById('nav-detail'); detail.replaceChildren();
  const legend = document.createElement('p'); legend.textContent = 'Click a box to inspect. Drag to arrange. Scroll to zoom; drag the background to pan.'; detail.appendChild(legend);
  Object.entries(navColors).filter(([kind]) => !['sends','implements'].includes(kind)).forEach(([kind,color]) => {
    const line = document.createElement('p'); line.style.color = color; line.textContent = kind; detail.appendChild(line);
  });
  errors.forEach(error => { const line = document.createElement('p'); line.textContent = error; detail.appendChild(line); });
  springGraph([...nodes.values()],edges);
}
function springGraph(nodes, edges) {
  cancelAnimationFrame(navFrame);
  const svg = document.getElementById('nav-graph'), ns = 'http://www.w3.org/2000/svg';
  svg.replaceChildren();
  const defs = document.createElementNS(ns,'defs');
  const marker = document.createElementNS(ns,'marker');
  Object.entries({id:'nav-arrow',viewBox:'0 0 10 10',refX:'10',refY:'5',markerWidth:'7',markerHeight:'7',orient:'auto-start-reverse'}).forEach(([k,v])=>marker.setAttribute(k,v));
  const arrow = document.createElementNS(ns,'path'); arrow.setAttribute('d','M 0 0 L 10 5 L 0 10 z'); arrow.setAttribute('fill','#777'); marker.appendChild(arrow); defs.appendChild(marker); svg.appendChild(defs);
  const scene = document.createElementNS(ns,'g'); svg.appendChild(scene);
  const width = svg.clientWidth || 700, height = svg.clientHeight || 500;
  let zoom = 1, panX = width/2, panY = height/2, drag = null, moved = false, ticks = 0;
  const transform = () => scene.setAttribute('transform','translate('+panX+','+panY+') scale('+zoom+')'); transform();
  const fit = () => {
    if (!nodes.length) return; else {}
    const xs=nodes.map(n=>n.x),ys=nodes.map(n=>n.y),minX=Math.min(...xs)-110,maxX=Math.max(...xs)+110,minY=Math.min(...ys)-45,maxY=Math.max(...ys)+45;
    zoom=Math.min(1,width/(maxX-minX),height/(maxY-minY))*.9;panX=width/2-(minX+maxX)/2*zoom;panY=height/2-(minY+maxY)/2*zoom;transform();
  };
  document.getElementById('nav-fit').onclick=fit;
  const map = new Map();
  nodes.forEach((n,i) => { n.x = Math.cos(i*2.399)*Math.sqrt(i+1)*65; n.y = Math.sin(i*2.399)*Math.sqrt(i+1)*65; n.vx=0; n.vy=0; map.set(n.id,n); });
  fit();
  const links = edges.filter(e=>map.has(e.from)&&map.has(e.to)).map(e=>{
    const line = document.createElementNS(ns,'path'); line.setAttribute('fill','none'); line.setAttribute('stroke',navColors[e.kind] || '#999'); line.setAttribute('marker-end','url(#nav-arrow)');
    if (e.kind === 'placement') line.setAttribute('stroke-dasharray','5 5'); else {}
    const text = document.createElementNS(ns,'text'); text.textContent=e.label; text.setAttribute('font-size','10'); text.setAttribute('fill',navColors[e.kind]||'#777');
    scene.append(line,text); return {...e,a:map.get(e.from),b:map.get(e.to),line,text};
  });
  nodes.forEach(n=>{
    const box = document.createElementNS(ns,'g'); box.setAttribute('tabindex','0'); box.setAttribute('role','button'); box.setAttribute('aria-label',n.label);
    const rect = document.createElementNS(ns,'rect'); Object.entries({x:-90,y:-23,width:180,height:46,rx:6,fill:'#fff',stroke:'#57748a'}).forEach(([k,v])=>rect.setAttribute(k,v)); box.appendChild(rect);
    n.label.split('\n').forEach((label,i)=>{ const text=document.createElementNS(ns,'text'); text.setAttribute('text-anchor','middle'); text.setAttribute('y',i*14-2); text.setAttribute('font-size','11'); text.textContent=label.length>27?label.slice(0,26)+'…':label; box.appendChild(text); });
    const title = document.createElementNS(ns,'title'); title.textContent=n.label; box.appendChild(title);
    box.onpointerdown=event=>{event.stopPropagation(); drag={node:n,x:event.clientX,y:event.clientY}; moved=false; svg.setPointerCapture(event.pointerId);};
    box.onkeydown=event=>{if(event.key==='Enter' && n.action) n.action(); else {}};
    scene.appendChild(box); n.box=box;
  });
  svg.onpointerdown=event=>{drag={x:event.clientX,y:event.clientY};moved=false;svg.setPointerCapture(event.pointerId);};
  svg.onpointermove=event=>{
    if (!drag) return; else {}
    const dx=event.clientX-drag.x,dy=event.clientY-drag.y;
    if(Math.abs(dx)+Math.abs(dy)>2) moved=true; else {}
    if(drag.node){drag.node.x+=dx/zoom;drag.node.y+=dy/zoom;drag.node.fixed=true;}else{panX+=dx;panY+=dy;transform();}
    drag.x=event.clientX;drag.y=event.clientY; draw();
  };
  svg.onpointerup=()=>{if(drag && drag.node && !moved && drag.node.action) drag.node.action(); else {} drag=null;};
  svg.onpointercancel=()=>{drag=null;};
  svg.onwheel=event=>{event.preventDefault();const rect=svg.getBoundingClientRect(),x=event.clientX-rect.left,y=event.clientY-rect.top;const next=Math.max(.08,Math.min(4,zoom*Math.exp(-event.deltaY*.001)));panX=x-(x-panX)*next/zoom;panY=y-(y-panY)*next/zoom;zoom=next;transform();};
  function draw(){
    nodes.forEach(n=>n.box.setAttribute('transform','translate('+n.x+','+n.y+')'));
    links.forEach(e=>{
      const dx=e.b.x-e.a.x,dy=e.b.y-e.a.y;
      const siblings=links.filter(other=>(other.from===e.from && other.to===e.to)||(other.from===e.to && other.to===e.from));
      const bend=siblings.length>1?25+siblings.filter(other=>other.from===e.from && other.to===e.to).indexOf(e)*22:0;
      const length=Math.max(1,Math.hypot(dx,dy)),cx=(e.a.x+e.b.x)/2-dy/length*bend,cy=(e.a.y+e.b.y)/2+dx/length*bend;
      const fraction=Math.min(90/Math.max(Math.abs(dx),.01),23/Math.max(Math.abs(dy),.01),.45);
      if(e.a===e.b){e.line.setAttribute('d','M '+(e.a.x+70)+' '+(e.a.y-23)+' c 70 -70 -70 -70 0 0');}else{e.line.setAttribute('d','M '+e.a.x+' '+e.a.y+' Q '+cx+' '+cy+' '+(e.b.x-dx*fraction)+' '+(e.b.y-dy*fraction));}
      e.text.setAttribute('x',cx);e.text.setAttribute('y',cy-5);
    });
  }
  function step(){
    nodes.forEach(n=>{n.vx-=n.x*.0008;n.vy-=n.y*.0008;});
    for(let i=0;i<nodes.length;i++) for(let j=i+1;j<nodes.length;j++){
      const a=nodes[i],b=nodes[j],dx=b.x-a.x,dy=b.y-a.y,d2=Math.max(dx*dx+dy*dy,100),force=1800/d2;
      if(Math.abs(dx)<205 && Math.abs(dy)<65){const push=(65-Math.abs(dy))*.045;const sign=dy>=0?1:-1;a.vy-=push*sign;b.vy+=push*sign;}else{}
      a.vx-=dx*force/Math.sqrt(d2);a.vy-=dy*force/Math.sqrt(d2);b.vx+=dx*force/Math.sqrt(d2);b.vy+=dy*force/Math.sqrt(d2);
    }
    links.forEach(e=>{const dx=e.b.x-e.a.x,dy=e.b.y-e.a.y,d=Math.max(1,Math.hypot(dx,dy)),f=(d-240)*.006;e.a.vx+=dx/d*f;e.a.vy+=dy/d*f;e.b.vx-=dx/d*f;e.b.vy-=dy/d*f;});
    nodes.forEach(n=>{n.vx*=.8;n.vy*=.8;if(!n.fixed && (!drag || drag.node!==n)){n.x+=n.vx;n.y+=n.vy;}else{}});
    draw(); if(ticks===100 && !drag) fit(); else {} if(++ticks<350 && !document.getElementById('navigation').hidden) navFrame=requestAnimationFrame(step); else {}
  }
  step();
}
document.addEventListener('keydown',event=>{if(event.key==='Escape' && !document.getElementById('navigation').hidden) closeNavigation(); else {}});
`
