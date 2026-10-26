package main

// uiHTML is the single-page web REPL served at / by the coordinator.
const uiHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>servicetalk</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font:13px/1.5 Menlo,Consolas,monospace;background:#f5f5f5;color:#1a1a1a;display:flex;flex-direction:column;height:100vh;overflow:hidden}

#hdr{padding:7px 14px;background:#fff;border-bottom:1px solid #ddd;display:flex;align-items:center;gap:14px;flex-shrink:0}
#hdr h1{color:#c0392b;font-size:13px;letter-spacing:2px}
#hdr small{color:#888;font-size:11px}

#main{display:flex;flex:1;overflow:hidden}

/* ---- left: editor ---- */
#left{display:flex;flex-direction:column;width:48%;border-right:1px solid #ddd;min-width:200px}
#code{flex:1;background:#fff;color:#1a1a1a;border:none;padding:12px;font:13px/1.7 Menlo,Consolas,monospace;resize:none;outline:none;tab-size:2}
#code::placeholder{color:#bbb}
#bar{background:#f0f0f0;border-top:1px solid #ddd;padding:6px 10px;display:flex;gap:8px;align-items:center;flex-shrink:0}
.btn{background:#fff;color:#333;border:1px solid #ccc;padding:3px 10px;cursor:pointer;font:12px Menlo,monospace}
.btn:hover{background:#e8e8e8}
.btn-run{background:#c0392b;border-color:#a93226;color:#fff}
.btn-run:hover{background:#a93226}
#hint{color:#aaa;font-size:11px}

/* ---- right: output ---- */
#right{flex:1;display:flex;flex-direction:column;overflow:hidden}
#out{flex:1;overflow-y:auto;padding:10px 12px;background:#fff}
.ev{margin-bottom:10px;border-left:2px solid #ddd;padding-left:10px}
.ev-src{color:#888;font-size:11px;margin-bottom:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ev-res{color:#1a1a1a;white-space:pre-wrap;word-break:break-all}
.ev-err{color:#c0392b;white-space:pre-wrap}
.ev-wait{color:#aaa;font-style:italic}

/* ---- bottom: objects ---- */
#objs{height:160px;border-top:1px solid #ddd;display:flex;flex-direction:column;flex-shrink:0}
#objs-hdr{padding:4px 12px;background:#f0f0f0;color:#555;font-size:11px;display:flex;justify-content:space-between;border-bottom:1px solid #ddd;flex-shrink:0}
#objs-scroll{flex:1;overflow-y:auto;background:#fff}
table{width:100%;border-collapse:collapse;font-size:12px}
th{padding:3px 10px;text-align:left;background:#f0f0f0;color:#555;font-weight:normal;position:sticky;top:0}
td{padding:3px 10px;border-bottom:1px solid #eee;color:#1a1a1a}
tr:hover td{background:#f5f5f5;cursor:pointer}
.cn{color:#c0392b}.oid{color:#888;font-size:11px}.pt{color:#555}

/* ---- inspect modal ---- */
#mbg{display:none;position:fixed;inset:0;background:rgba(0,0,0,.4);z-index:10}
#modal{display:none;position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);background:#fff;border:1px solid #ccc;padding:18px 20px;min-width:420px;max-width:620px;max-height:78vh;overflow-y:auto;z-index:11}
#modal h3{color:#c0392b;margin-bottom:12px;font-size:13px;word-break:break-all}
.iv{display:flex;gap:10px;padding:4px 0;border-bottom:1px solid #eee;font-size:12px}
.ivk{color:#2471a3;min-width:130px;word-break:break-word}
.ivv{color:#1a1a1a;word-break:break-all}
.mths{color:#555;font-size:11px;margin-top:10px;line-height:1.9}
.mths span{display:inline-block;background:#f0f0f0;border:1px solid #ddd;padding:1px 7px;margin:2px}
</style>
</head>
<body>

<div id="hdr">
  <h1>servicetalk</h1>
  <small>workspace — state persists between evals — ctrl+enter to run</small>
</div>

<div id="main">
  <div id="left">
    <textarea id="code">"Spawn a counter and poke it"
c := Counter new.
c increment.
c increment.
c add: 10.
c value</textarea>
    <div id="bar">
      <button class="btn btn-run" onclick="doEval()">&#9654; Eval</button>
      <button class="btn" onclick="document.getElementById('out').innerHTML=''">&#x2715; Clear</button>
      <span id="hint">ctrl+enter</span>
    </div>
  </div>
  <div id="right">
    <div id="out"></div>
  </div>
</div>

<div id="objs">
  <div id="objs-hdr"><span>Live Objects</span><span id="ocount">&ndash;</span></div>
  <div id="objs-scroll">
    <table>
      <thead><tr><th>Class</th><th>ID</th><th>Subject</th></tr></thead>
      <tbody id="otbody"></tbody>
    </table>
  </div>
</div>

<div id="mbg" onclick="closeM()"></div>
<div id="modal">
  <h3 id="mtitle"></h3>
  <div id="mruntime" style="white-space:pre-wrap;overflow-wrap:anywhere;margin-bottom:12px"></div>
  <div id="mvars"></div>
  <div class="mths" id="mmths"></div>
</div>

<script>
const code = document.getElementById('code');
const out  = document.getElementById('out');

code.addEventListener('keydown', e => {
  if (e.ctrlKey && e.key === 'Enter') { e.preventDefault(); doEval(); }
});

async function doEval() {
  const src = code.value.trim();
  if (!src) return;

  const el = document.createElement('div');
  el.className = 'ev';
  const preview = src.split('\n')[0].slice(0,72) + (src.length > 72 || src.includes('\n') ? ' \u2026' : '');
  el.innerHTML = '<div class="ev-src">\u276f ' + esc(preview) + '</div>'
               + '<div class="ev-wait">evaluating\u2026</div>';
  out.appendChild(el);
  out.scrollTop = out.scrollHeight;

  try {
    const r = await fetch('/eval', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({code: src})
    });
    const d = await r.json();
    const res = el.querySelector('.ev-wait');
    if (d.error) {
      res.className = 'ev-err';
      res.textContent = 'error: ' + d.error;
    } else {
      res.className = 'ev-res';
      res.textContent = d.output;
    }
  } catch(e) {
    el.querySelector('.ev-wait').className = 'ev-err';
    el.querySelector('.ev-wait').textContent = 'network error: ' + e.message;
  }
  out.scrollTop = out.scrollHeight;
}

async function refreshObjs() {
  try {
    const r = await fetch('/registry');
    const d = await r.json();
    const entries = (d.registry || []).sort((a,b) => (a.id || '').localeCompare(b.id || ''));
    document.getElementById('ocount').textContent =
      entries.length + ' object' + (entries.length !== 1 ? 's' : '');
    const tb = document.getElementById('otbody');
    tb.innerHTML = '';
    entries.forEach(e => {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td class="cn">' + esc(e.className) + '</td>'
                   + '<td class="oid">' + e.id + '</td>'
                   + '<td class="pt">'  + esc(e.subject || '') + '</td>';
      tr.onclick = () => showInspect(e.id);
      tb.appendChild(tr);
    });
  } catch(_) {}
}

async function showInspect(id) {
  try {
    const r = await fetch('/inspect?id=' + encodeURIComponent(id));
    if (!r.ok) return;
    const d = await r.json();
    document.getElementById('mtitle').textContent = d.className + '   ' + id;
    const runtime = d.runtime || {};
    const retention = d.retention || {};
    const details = [
      'Host: ' + (runtime.host || 'unknown') + '  PID: ' + (runtime.pid || 'unknown'),
      'Placement: ' + (runtime.workerId ? 'worker ' + runtime.workerId : 'coordinator host'),
      'Mailbox: ' + ((runtime.metrics || {}).mailboxDepth || 0),
      retention.root ? 'Retention: GC root' : 'Root path: ' + ((retention.path || []).join(' → ') || 'no visible path'),
      retention.note || '',
      (retention.unavailable || []).length ? 'Unavailable: ' + retention.unavailable.join(', ') : '',
      runtime.foreignReferences ? 'Foreign-held references require the class pulse handler.' : '',
      'Active calls and waits:',
      ...(runtime.calls || []).map(c => c.state + ' ' + c.method + ' → ' + c.target + '  since ' + c.started + '  chain ' + c.chainId)
    ];
    document.getElementById('mruntime').textContent = details.filter(Boolean).join('\n');

    const vd = document.getElementById('mvars');
    vd.innerHTML = '';
    const vars = d.instanceVars || {};
    const keys = Object.keys(vars);
    if (keys.length === 0) {
      vd.innerHTML = '<div style="color:#2a4a6a;font-size:12px">(no variables)</div>';
    } else {
      keys.forEach(k => {
        const row = document.createElement('div');
        row.className = 'iv';
        row.innerHTML = '<span class="ivk">' + esc(k) + '</span>'
                      + '<span class="ivv">' + esc(fmtVal(vars[k])) + '</span>';
        vd.appendChild(row);
      });
    }

    const methods = d.methods || [];
    const md = document.getElementById('mmths');
    md.innerHTML = methods.length
      ? methods.map(m => '<span>' + esc(m) + '</span>').join('')
      : '';

    document.getElementById('mbg').style.display = 'block';
    document.getElementById('modal').style.display = 'block';
  } catch(_) {}
}

function fmtVal(v) {
  if (!v || v.kind === 'nil') return 'nil';
  switch(v.kind) {
    case 'number': return v.num === Math.floor(v.num) ? String(v.num|0) : String(v.num);
    case 'string': return '"' + v.str + '"';
    case 'objid':  return '#' + v.objid;
    case 'bool':   return String(v.bool);
  }
  return 'nil';
}

function closeM() {
  document.getElementById('mbg').style.display = 'none';
  document.getElementById('modal').style.display = 'none';
}

function esc(s) {
  return String(s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;')
    .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

document.addEventListener('keydown', e => { if (e.key === 'Escape') closeM(); });
setInterval(refreshObjs, 2000);
refreshObjs();
</script>
</body>
</html>`
