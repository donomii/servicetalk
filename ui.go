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
#modal{display:none;position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);background:#fff;border:1px solid #ccc;padding:18px 20px;width:min(620px,calc(100vw - 24px));max-height:78vh;overflow-y:auto;z-index:11}
#modal h3{color:#c0392b;margin-bottom:12px;font-size:13px;word-break:break-all}
.iv{display:flex;gap:10px;padding:4px 0;border-bottom:1px solid #eee;font-size:12px}
.ivk{color:#2471a3;min-width:130px;word-break:break-word}
.ivv{color:#1a1a1a;word-break:break-all}
.mths{color:#555;font-size:11px;margin-top:10px;line-height:1.9}
.mths button{display:inline-block;background:#f0f0f0;border:1px solid #ddd;padding:1px 7px;margin:2px}
#mcall{margin-top:12px}#mcall input,#mcall select{max-width:100%;padding:5px;font:inherit}#mselector{width:100%}.marg{display:flex;gap:6px;margin:6px 0}.marg input{flex:1;min-width:0}#mresult{white-space:pre-wrap;overflow-wrap:anywhere;margin-top:10px}.objlink{border:0;background:none;color:#2471a3;text-decoration:underline;cursor:pointer}
#navigation{position:fixed;inset:8px;background:#fff;z-index:8;border:1px solid #bbb;display:flex;flex-direction:column}#navigation[hidden]{display:none}#nav-toolbar{display:flex;gap:6px;padding:8px;flex-wrap:wrap}#nav-search{min-width:80px;flex:1}#nav-body{display:flex;flex:1;min-height:0}#nav-graph{flex:1;min-width:0;background:#f6f8fa;touch-action:none}#nav-detail{width:38%;min-width:180px;padding:10px;overflow:auto;overflow-wrap:anywhere}#nav-detail h4{margin:12px 0 6px}#nav-detail .btn{margin:3px;white-space:normal;text-align:left}#nav-detail pre{max-height:50vh;overflow:auto;font-size:12px;margin-top:10px}.source-current{background:#fff0b4}@media(max-width:650px){#nav-body{flex-direction:column}#nav-graph{min-height:45%}#nav-detail{width:100%;height:50%}}
#ow{margin-top:16px;border-top:1px solid #ddd;padding-top:10px}#ow h4{margin-bottom:6px}#ow-code{display:block;width:100%;min-height:110px;resize:vertical;font:inherit;padding:8px;margin:8px 0}#ow-method{max-width:100%;padding:4px}#ow-args .marg{flex-wrap:wrap}#ow-args input{min-width:90px}#ow-result{white-space:pre-wrap;overflow-wrap:anywhere;margin-top:8px}#ow-result pre{white-space:pre-wrap}#ow-suggestions .btn{margin:4px}#ow-signature,#ow-target{color:#555}
.result-actions{display:flex;gap:5px;flex-wrap:wrap;margin:7px 0}.result-actions input{max-width:150px;min-width:90px}.ev-res pre{white-space:pre-wrap}#recording-panel{position:fixed;inset:20px;background:white;border:1px solid #ccc;padding:12px;z-index:12;display:flex;flex-direction:column;gap:10px}#recording-panel[hidden]{display:none}#recording-list{max-height:35%;overflow:auto;overflow-wrap:anywhere}.record-line{padding:6px;border-bottom:1px solid #eee}#recording-source{flex:1;min-height:140px;font:inherit;padding:10px;resize:none}#hdr{flex-wrap:wrap}
.view-tabs,.view-tools,.view-row{display:flex;gap:7px;flex-wrap:wrap}.view-tools{margin:8px 0}.view-node{min-width:0;max-width:100%;margin:6px 0}.view-node>label{display:block;font-weight:bold}.view-node pre{white-space:pre-wrap;overflow-wrap:anywhere}.view-column{display:flex;flex-direction:column}.view-body{background:#f8fafb;border:1px solid #ddd;padding:9px;margin:6px 0}.view-value>span{font-size:24px}.view-table{max-width:100%;overflow:auto}.view-table td{overflow-wrap:anywhere;max-width:260px}.view-editor{width:100%;min-height:230px;font:inherit}.view-error{color:#b33}.view-textarea textarea{width:100%;min-height:90px}
#result-shelf{position:fixed;inset:20px;background:white;border:1px solid #ccc;padding:12px;z-index:12;overflow:auto}#result-shelf[hidden]{display:none}.result-card{padding:10px 0;border-bottom:1px solid #ddd;overflow-wrap:anywhere}
</style>
<style>
#wiring-panel{display:flex;flex-direction:column}#wiring-panel[hidden]{display:none}#wiring-body{display:flex;flex:1;min-height:0;gap:12px}#wiring-graph{flex:1;min-width:0;min-height:300px;background:#f4f7fa;touch-action:none}#wiring-detail{width:33%;overflow:auto;min-width:220px;padding:8px}#wiring-detail input{max-width:100%}#wiring-detail .btn{margin:6px}#wiring-object{min-width:100px;max-width:400px}#wiring-status{overflow-wrap:anywhere}@media(max-width:650px){#wiring-body{flex-direction:column;overflow:auto}#wiring-graph{min-height:45vh}#wiring-detail{width:100%;overflow:visible}}
</style>
<style>
#source-panel{display:flex;flex-direction:column;gap:10px}#source-panel[hidden]{display:none}#source-title{overflow-wrap:anywhere}#source-body{display:grid;grid-template-columns:minmax(0,3fr) minmax(0,2fr);gap:14px;flex:1;min-height:0}#source-editor{width:100%;height:100%;resize:none;min-height:200px}#source-history-pane{overflow:auto;min-width:0}#source-history{display:flex;flex-direction:column;gap:5px}#source-history button{white-space:normal;text-align:left}#source-preview{white-space:pre-wrap;overflow-wrap:anywhere;padding:8px;background:#f6f8fa}#source-message{flex:1;min-width:100px}@media(max-width:650px){#source-body{grid-template-columns:minmax(0,1fr);overflow:auto}#source-editor{height:45vh}#source-history-pane{overflow:visible}}
</style>
<style>
.runtime-panel{position:fixed;inset:12px;z-index:14;background:#fff;border:1px solid #bbb;padding:14px;overflow:auto}.runtime-panel[hidden]{display:none}.runtime-toolbar{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-bottom:12px}.runtime-panel pre{white-space:pre-wrap;overflow-wrap:anywhere}.runtime-panel input,.runtime-panel select,.runtime-panel textarea{max-width:100%;font:inherit;padding:5px}.runtime-panel .marg{flex-wrap:wrap}.runtime-panel .marg label{min-width:100px}.runtime-panel .marg input{min-width:70px}.runtime-status{white-space:pre-wrap;margin:10px 0;color:#555}.debug-columns{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:20px}.debug-current{background:#fff0b4;border-left:3px solid #c0392b;padding:7px}#debugger-code{width:100%;min-height:90px}#debugger-statements{max-height:35vh;overflow:auto}.trace-event{border-bottom:1px solid #ddd;padding:12px 0;overflow-wrap:anywhere}.trace-heading,.trace-endpoints{display:flex;gap:12px;flex-wrap:wrap}.trace-heading{justify-content:space-between}.trace-event small{display:block;color:#888;margin-top:6px}@media(max-width:650px){.debug-columns{grid-template-columns:minmax(0,1fr)}}
</style>
<style>
#examples-body{display:grid;grid-template-columns:220px minmax(0,1fr);gap:16px}#examples-list{display:flex;flex-direction:column;gap:8px}#examples-list .btn{text-align:left;white-space:normal}#example-editor label{display:block;margin-bottom:10px}#example-editor input,#example-editor textarea{display:block;width:100%}#example-source{min-height:200px}#example-check{min-height:85px}#example-description{min-height:65px}#example-result{overflow-wrap:anywhere}#example-result pre{white-space:pre-wrap}@media(max-width:650px){#examples-body{grid-template-columns:minmax(0,1fr)}#examples-list{flex-direction:row;flex-wrap:wrap}}
</style>
<style>
#view-designer{z-index:16}.visual-columns{display:grid;grid-template-columns:minmax(180px,1fr) minmax(0,2fr);gap:24px}.visual-design-columns{display:grid;grid-template-columns:minmax(150px,1fr) minmax(220px,1.3fr) minmax(0,2fr);gap:24px}.visual-columns section,.visual-design-columns section{min-width:0;overflow-wrap:anywhere}.visual-field{display:flex;flex-direction:column;gap:4px;margin:10px 0}.visual-field input,.visual-field select{width:100%;min-width:0}.visual-arguments .marg input{width:100%;min-width:0}.visual-arguments .marg select{min-width:0}.visual-arguments h4{margin-bottom:8px}#tool-outline,#view-design-tree{display:flex;flex-direction:column;gap:5px;margin-bottom:12px}#tool-outline button,#view-design-tree button{white-space:normal;text-align:left;overflow-wrap:anywhere}#tool-outline button[aria-pressed=true],#view-design-tree button[aria-pressed=true]{background:#e3eefb;border-color:#3673ad}#view-design-preview{margin-top:12px;overflow-wrap:anywhere}#view-design-preview .visual-preview-control{border:1px dashed #b9c5d1;padding:8px;cursor:pointer;min-width:0}#view-design-preview .visual-selected{outline:2px solid #3673ad;outline-offset:-2px;background:#f1f7ff}#view-design-preview .view-row{flex-wrap:wrap}#view-design-preview .marg input{min-width:0;width:100%}#view-design-preview .view-table{overflow:auto}.visual-design-columns h4,.visual-columns h4{margin:8px 0 12px}@media(max-width:1000px){.visual-design-columns{grid-template-columns:minmax(0,1fr) minmax(0,2fr)}.visual-design-columns>section:last-child{grid-column:1 / -1}}@media(max-width:650px){.visual-columns,.visual-design-columns{grid-template-columns:minmax(0,1fr)}.visual-design-columns>section:last-child{grid-column:auto}}
</style>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>` + workbenchThemeCSS + `</style>
</head>
<body>

<header id="hdr"><h1>servicetalk</h1><span id="workbench-name">Workbench</span><span id="connection-status" role="status">Connecting</span></header>
<aside id="tool-rail" aria-label="Workbench tools"><span class="rail-label">Tools</span><nav id="workbench-tools"></nav><span id="recording-status"></span><span id="workbench-status"></span><div class="rail-config"><button class="btn" onclick="openWorkbench()">Customize tools</button></div></aside>
<div class="page-heading"><h2>Playground</h2><span>Smalltalk workspace</span></div>

<div id="main">
  <div id="left">
    <div class="pane-heading"><strong>Expression</strong><select id="playground-starter" aria-label="Load a starter expression" onchange="loadPlaygroundStarter(this.value)"><option value="">Load starter…</option><option value="counter">An object and messages</option><option value="files">Explore files</option><option value="command">Run a command</option><option id="playground-restore" value="restore" hidden>Restore my expression</option></select><kbd>Ctrl + Enter</kbd></div>
    <textarea id="code">"Spawn a counter and poke it"
c := Counter new.
c increment.
c increment.
c add: 10.
c value</textarea>
    <div id="bar">
      <button class="btn btn-run" onclick="doEval()">Run expression</button>
      <button class="btn" onclick="clearResults()">Clear output</button>
      <span id="hint">ctrl+enter</span>
    </div>
  </div>
  <div id="right">
    <div class="pane-heading"><strong>Output</strong></div>
    <div id="out"></div>
  </div>
</div>

<div id="objs">
  <div id="objs-hdr"><span><strong>Live objects</strong><span id="ocount">&ndash;</span></span><input id="object-filter" aria-label="Find live objects" placeholder="Find class or object…" oninput="renderObjectList()"><label><input id="object-internals" type="checkbox" onchange="renderObjectList()">Runtime objects</label></div>
  <div id="objs-scroll">
    <table>
      <thead><tr><th>Class</th><th>Object</th><th aria-label="Actions"></th></tr></thead>
      <tbody id="otbody"></tbody>
    </table>
  </div>
</div>

<section id="navigation" hidden><div id="nav-toolbar"><button class="btn" onclick="navMode='methods';refreshNavigation()">Methods</button><button class="btn" onclick="navMode='objects';refreshNavigation()">Objects</button><input id="nav-search" aria-label="Find class or selector" placeholder="Class or selector" oninput="if(navMode==='methods')renderNavigation()"><button class="btn" id="nav-fit">Fit graph</button><button class="btn" onclick="refreshNavigation()">Refresh</button><button class="btn" onclick="closeNavigation()">Close</button></div><div id="nav-body"><svg id="nav-graph" aria-label="Relationship graph"></svg><div id="nav-detail"></div></div></section>
<section id="result-shelf" hidden><button class="btn" onclick="document.getElementById('result-shelf').hidden=true">Close results</button><div id="result-shelf-list"></div></section>
<datalist id="result-choices"></datalist>
<section id="recording-panel" hidden><div><button class="btn" id="recording-script">Script</button><button class="btn" id="recording-test">Test</button><button class="btn" id="recording-download">Download</button><button class="btn" id="recording-load">Load into REPL</button><button class="btn" onclick="document.getElementById('recording-panel').hidden=true">Close</button></div><div id="recording-list"></div><textarea id="recording-source" aria-label="Recorded script"></textarea></section>
<div id="mbg" onclick="closeM()"></div>
<section id="refactor-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong>Rename selector</strong><button class="btn" onclick="closeRefactor()">Close</button></div><div class="runtime-toolbar"><label>From <input id="refactor-from" aria-label="Original selector"></label><label>To <input id="refactor-to" aria-label="New selector"></label><label>Scope <select id="refactor-scope" aria-label="Refactoring scope"></select></label><button class="btn" onclick="previewRefactor()">Preview changes</button></div><div id="refactor-results"></div></section>
<section id="workbench-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong>Workbench</strong><button class="btn" onclick="saveWorkbench()">Save tools</button><button class="btn" onclick="openWorkbench(true)">Reload</button><button class="btn" onclick="resetWorkbench()">Restore default tools</button><button class="btn" onclick="inspectWorkbench()">Inspect workbench object</button><button class="btn" id="tool-mode" onclick="toggleToolEditor()">Edit JSON</button><button class="btn" onclick="closeToolEditor()">Close</button></div><div id="workbench-message" class="runtime-status" role="status"></div><div id="workbench-visual"></div><textarea id="workbench-editor" aria-label="Workbench definition" style="width:100%;height:65vh" hidden></textarea></section>
<section id="view-designer" class="runtime-panel" hidden><div class="runtime-toolbar"><strong>View designer</strong><button class="btn" onclick="saveViewDesigner()">Save layout</button><button class="btn" onclick="reloadViewDesigner()">Reload saved layout</button><button class="btn" id="view-design-mode" onclick="toggleViewDesignerJSON()">Edit JSON</button><button class="btn" onclick="closeViewDesigner()">Close</button></div><div id="view-designer-status" class="runtime-status" role="status"></div><div id="view-design-visual"><div class="runtime-toolbar"><select id="view-control-kind" aria-label="New control kind"><option>text</option><option>value</option><option>action</option><option>input</option><option>slider</option><option>textarea</option><option>row</option><option>column</option><option>table</option><option>list</option><option>code</option></select><button class="btn" onclick="addViewControl()">Add control</button><button class="btn" id="view-control-up" onclick="changeViewControl('up')">Move up</button><button class="btn" id="view-control-down" onclick="changeViewControl('down')">Move down</button><button class="btn" onclick="changeViewControl('in')">Nest in previous</button><button class="btn" id="view-control-out" onclick="changeViewControl('out')">Move out</button><button class="btn" id="view-control-copy" onclick="changeViewControl('copy')">Duplicate</button><button class="btn" id="view-control-remove" onclick="changeViewControl('remove')">Remove control</button></div><div class="visual-design-columns"><section><h4>Layout</h4><nav id="view-design-tree"></nav></section><section id="view-design-properties"></section><section><h4>Preview · click a control to edit</h4><button class="btn" onclick="previewViewDesigner()">Refresh preview</button><div id="view-design-preview"></div></section></div></div><textarea id="view-design-json" aria-label="View layout JSON" class="view-editor" hidden></textarea></section>
<section id="examples-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong>Examples</strong><button class="btn" onclick="newExample()">New example</button><button class="btn" onclick="inspectExample()">Inspect example</button><button class="btn" onclick="inspectExample(true)">Inspect workspace</button><button class="btn" onclick="closeExamples()">Close</button></div><div id="examples-body"><nav><input id="examples-search" aria-label="Find examples" placeholder="Find examples…" oninput="renderExampleList()"><div id="examples-list"></div></nav><div id="example-editor"><label>Title<input id="example-title" aria-label="Example title"></label><label>Description<textarea id="example-description" aria-label="Example description"></textarea></label><div id="example-observed" class="example-observed"></div><label>Source<textarea id="example-source" aria-label="Example source"></textarea></label><label>Check<textarea id="example-check" aria-label="Example check"></textarea></label><div class="runtime-toolbar"><button class="btn" id="example-save">Save example</button><button class="btn btn-run" id="example-run" onclick="runExample()">Run and check</button><button class="btn" onclick="resetExample()">Reset workspace</button></div><div id="examples-status" class="runtime-status"></div><div id="example-result"></div></div></div></section>
<section id="wiring-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong id="wiring-owner">Message graph</strong><button class="btn" id="wiring-inspect">Inspect graph object</button><button class="btn" onclick="closeWiring()">Close</button></div><div class="runtime-toolbar"><select id="wiring-object" aria-label="New node receiver"></select><button class="btn" onclick="addWiringNode()">Add node</button><button class="btn" id="wiring-save">Save graph</button><button class="btn" onclick="runWiring()">Run graph</button><button class="btn" onclick="runWiringPromise()">Run asynchronously</button><button class="btn" onclick="runWiring(true)">Resume from failure</button><button class="btn" onclick="fitWiring();renderWiring()">Fit graph</button></div><div id="wiring-status" class="runtime-status"></div><div id="wiring-body"><svg id="wiring-graph" aria-label="Message wiring graph"></svg><div id="wiring-detail"></div></div><datalist id="wiring-node-options"></datalist><datalist id="wiring-method-options"></datalist></section>
<section id="clone-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong id="clone-title">Clone and edit</strong><button class="btn" onclick="closeClone()">Close</button></div><p>The copy gets its own fields and loaded methods. Object references stay shared.</p><div id="clone-status" class="runtime-status"></div><div class="debug-columns"><div><h4>Fields</h4><div id="clone-fields"></div></div><div><h4>Method override (optional)</h4><label>Selector <input id="clone-method" list="clone-method-choices" oninput="cloneMethodSelected()"></label><datalist id="clone-method-choices"></datalist><label>Parameters, separated by commas <input id="clone-params"></label><textarea id="clone-code" aria-label="Derived method body" style="display:block;width:100%;min-height:160px;margin:12px 0" placeholder="Smalltalk method body"></textarea></div></div><button class="btn" onclick="deriveClone()">Create derived object</button></section>
<section id="source-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong id="source-title"></strong><button class="btn" id="source-save" onclick="saveSource()">Save definition</button><button class="btn" onclick="sourceDiff()">Working diff</button><button class="btn" onclick="openSource(sourceState.class)">Refresh history</button><button class="btn" onclick="closeSource()">Close</button></div><div id="source-status" class="runtime-status"></div><div id="source-body"><textarea id="source-editor" aria-label="Class definition" spellcheck="false" oninput="rememberSourceDraft()"></textarea><div id="source-history-pane"><h4>Git history</h4><div id="source-git"></div><div id="source-history"></div><button class="btn" id="source-restore" hidden onclick="restoreSourceRevision()">Copy revision to editor</button><pre id="source-preview"></pre></div></div><div class="runtime-toolbar"><input id="source-message" aria-label="Commit message" placeholder="Commit message"><button class="btn" id="source-commit" onclick="commitSource()">Commit this file</button></div></section>
<section id="debugger-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong id="debugger-target">Native method debugger</strong><button class="btn" onclick="refreshDebugger()">Refresh</button><button class="btn" onclick="closeDebugger()">Close</button></div><label>Method <select id="debugger-method"></select></label><div id="debugger-args"></div><button class="btn" id="debugger-start">Start paused</button><div class="runtime-status" id="debugger-status"></div><section id="debugger-frame" hidden><div class="debug-columns"><div><h4>Statements</h4><div id="debugger-statements"></div><label for="debugger-code">Replacement for current statement (optional)</label><textarea id="debugger-code"></textarea><div id="debugger-result"></div></div><div><h4>Arguments</h4><div id="debugger-bindings"></div><h4>Receiver fields</h4><div id="debugger-fields"></div><div id="debugger-return-value"></div></div></div><div id="debugger-actions" class="runtime-toolbar"><button class="btn" data-action="step">Step</button><button class="btn" data-action="continue">Continue</button><button class="btn" data-action="retry">Retry statement</button><button class="btn" data-action="return">Return value</button><button class="btn" data-action="abort">Abort</button></div></section></section>
<section id="trace-panel" class="runtime-panel" hidden><div class="runtime-toolbar"><strong>Message trace</strong><button class="btn" onclick="configureTrace('start')">Start tracing</button><button class="btn" onclick="configureTrace('stop')">Stop tracing</button><button class="btn" onclick="refreshTrace()">Refresh</button><button class="btn" onclick="configureTrace('clear')">Clear</button><button class="btn" onclick="exportTrace()">Download JSON</button><button class="btn" onclick="closeTrace()">Close</button></div><div class="runtime-toolbar"><input id="trace-filter" aria-label="Filter messages" placeholder="Method, object or signal" oninput="renderTrace()"><input id="trace-chain" aria-label="Call chain" placeholder="Call chain" oninput="renderTrace()"><button class="btn" onclick="document.getElementById('trace-chain').value='';renderTrace()">All chains</button></div><div id="trace-status" class="runtime-status"></div><div id="trace-list"></div></section>
<div id="modal" role="dialog" aria-modal="true" aria-labelledby="mtitle">
  <div id="inspect-header"><div class="inspect-heading"><div><span class="eyebrow">Live object</span><h3 id="mtitle"></h3><div id="midentity"></div></div><button class="btn" onclick="closeM()">Close</button></div><div id="object-help" class="object-help"></div><nav id="inspect-tabs" role="tablist" aria-label="Object sections"><button class="btn" id="inspect-tab-view" role="tab" aria-controls="inspect-view" onclick="selectInspectorSection('view')">Interface</button><button class="btn" id="inspect-tab-methods" role="tab" aria-controls="inspect-methods" onclick="selectInspectorSection('methods')">Methods</button><button class="btn" id="inspect-tab-state" role="tab" aria-controls="inspect-state" onclick="selectInspectorSection('state')">State</button><button class="btn" id="inspect-tab-workspace" role="tab" aria-controls="inspect-workspace" onclick="selectInspectorSection('workspace')">Workspace</button></nav></div>
  <section id="inspect-view" class="inspect-pane" role="tabpanel" aria-labelledby="inspect-tab-view"><div id="mviews"></div><div id="mview-result" role="status"></div></section>
  <section id="inspect-state" class="inspect-pane" role="tabpanel" aria-labelledby="inspect-tab-state" hidden><div id="mpromise"></div><div id="mproperties"></div><h4>Fields</h4><div id="mvars"></div><details><summary>Runtime details</summary><div id="mruntime" style="white-space:pre-wrap;overflow-wrap:anywhere;margin-bottom:12px"></div></details></section>
  <section id="inspect-methods" class="inspect-pane" role="tabpanel" aria-labelledby="inspect-tab-methods" hidden><input id="method-filter" aria-label="Find a method" placeholder="Find a method…" oninput="filterInspectorMethods()"><div class="mths" id="mmths"></div>
  <form id="mcall" onsubmit="sendObjectMessage(event)">
    <label for="mselector">Method</label>
    <input id="mselector" required oninput="prepareArguments()" placeholder="Select or enter a method">
    <div id="selector-help" class="selector-help"></div><div id="msignature"></div>
    <div id="margs"></div>
    <button class="btn btn-run" id="msend">Send</button>
    <button type="button" class="btn" id="mpromise-send" onclick="sendObjectPromise()">Send asynchronously</button>
  </form>
  <details><summary>Object tools</summary><div id="mactions"></div></details></section>
  <section id="inspect-workspace" class="inspect-pane" role="tabpanel" aria-labelledby="inspect-tab-workspace" hidden>
  <section id="ow"><h4>Object workspace</h4><div id="ow-target"></div><div id="ow-suggestions"></div><label>Build an expression <select id="ow-method" onchange="workspaceArguments()"></select></label><div id="ow-signature"></div><div id="ow-args"></div><button class="btn" onclick="insertWorkspaceMethod()">Insert method</button><textarea id="ow-code" aria-label="Object expression" oninput="rememberWorkspaceDraft()"></textarea><button class="btn btn-run" id="ow-run" onclick="runObjectWorkspace()">Run expression</button><span> Ctrl+Enter</span><div id="ow-result" role="status"></div></section></section>
  <div id="mresult" role="status"></div>
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

  removeStarterHelp();
  const el = document.createElement('div');
  el.className = 'ev';
  const preview = src.split('\n')[0].slice(0,72) + (src.length > 72 || src.includes('\n') ? ' \u2026' : '');
  el.innerHTML = '<div class="ev-src">\u276f ' + esc(preview) + '</div>'
               + '<div class="ev-wait">evaluating\u2026</div>';
  out.appendChild(el);
  out.scrollTop = out.scrollHeight;

  try {
    const d = await performInteraction({kind:'eval',code:src});
    const res = el.querySelector('.ev-wait');
    if (d.error) {
      res.className = 'ev-err';
      res.textContent = 'error: ' + d.error;
    } else {
      res.className = 'ev-res';
      renderResult(res, d);
    }
  } catch(e) {
    const failed = el.lastElementChild;
    failed.className = 'ev-err';
    failed.textContent = e.message;
  }
  out.scrollTop = out.scrollHeight;
}

async function refreshObjs() {
  try {
    const r = await fetch('/registry');
    const d = await r.json();
    objectEntries = (d.registry || []).sort((a,b) => (a.className || '').localeCompare(b.className || '') || (a.id || '').localeCompare(b.id || ''));
    renderObjectList();document.getElementById('connection-status').textContent='Connected';document.getElementById('connection-status').dataset.offline='false';
  } catch(error) { document.getElementById('connection-status').textContent='Disconnected';document.getElementById('connection-status').dataset.offline='true'; }
}

let inspectedId = null;
let methodSignatures = {};
let inspectorMethods = [];
let inspectorMetadata = {};
let inspectionVersion = 0;
async function showInspect(id, preserveCall = false) {
  if(!preserveCall&&document.getElementById('modal').style.display!=='block')inspectorReturnFocus=document.activeElement;else{}
  const version = ++inspectionVersion;
  inspectedId = id;
  if (!preserveCall) {
    document.getElementById('mselector').value = '';
    document.getElementById('margs').innerHTML = '';
    document.getElementById('msignature').textContent = '';
    document.getElementById('mresult').textContent = '';
    document.getElementById('mview-result').replaceChildren();
  }
  try {
    const r = await fetch('/inspect?id=' + encodeURIComponent(id));
    if (!r.ok) return;
    const d = await r.json();
    if (version !== inspectionVersion || inspectedId !== id) return;
    document.getElementById('mtitle').textContent = resultClassName(d.className);
    document.getElementById('midentity').textContent = id;
    const runtime = d.runtime || {};
    const debugBusy = runtime.debug && ['queued','running','paused'].includes(runtime.debug.state);
    const retention = d.retention || {};
    const details = [
      'Host: ' + (runtime.host || 'unknown') + '  PID: ' + (runtime.pid || 'unknown'),
      'Placement: ' + (runtime.workerId ? 'worker ' + runtime.workerId : 'coordinator host'),
      'Mailbox: ' + ((runtime.metrics || {}).mailboxDepth || 0),
      runtime.prototype ? 'Cloned from: ' + runtime.prototype : '',
      runtime.definitionVersion ? 'Definition: ' + runtime.definitionVersion.slice(0,12) : '',
      retention.root ? 'Retention: GC root' : 'Root path: ' + ((retention.path || []).join(' → ') || 'no visible path'),
      (retention.unavailable || []).length ? 'Unavailable: ' + retention.unavailable.join(', ') : '',
      runtime.foreignReferences ? 'Foreign-held references require the class pulse handler.' : '',
      'Active calls and waits:',
      ...(runtime.calls || []).map(c => c.state + ' ' + c.method + ' → ' + c.target + '  since ' + c.started + '  chain ' + c.chainId)
    ];
    document.getElementById('mruntime').textContent = details.filter(Boolean).join('\n');
    renderPromise(id,runtime.promise,version);

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
        if (vars[k] && vars[k].kind === 'objid') {
          const value = row.querySelector('.ivv');
          value.textContent = '';
          value.appendChild(objectLink(vars[k].objid));
        }
        vd.appendChild(row);
      });
    }

    const methods = [...(d.methods || [])].sort();inspectorMethods=methods;
    methodSignatures = d.methodSignatures || {};
    inspectorMetadata = {};
    document.getElementById('mproperties').textContent = '';
    if (!debugBusy && methods.includes('inspectorMetadata')) {
      try {
        const metadata = await inspectorCall(id, 'inspectorMetadata', []);
        if (version !== inspectionVersion || inspectedId !== id) return;
        if (metadata.kind !== 'string') throw Error('inspectorMetadata must return a JSON string');
        inspectorMetadata = JSON.parse(metadata.str);
        if (!inspectorMetadata || typeof inspectorMetadata !== 'object') throw Error('Invalid inspector metadata');
        await renderProperties(id, inspectorMetadata.properties || [], version);
      } catch (error) {
        if (version !== inspectionVersion || inspectedId !== id) return;
        inspectorMetadata = {};
        document.getElementById('mproperties').textContent = 'Properties: ' + error.message;
      }
    }
    if (version !== inspectionVersion || inspectedId !== id) return;
    const md = document.getElementById('mmths'),actions=document.getElementById('mactions');
    md.innerHTML = '';actions.replaceChildren();
    actions.appendChild(navButton('Browse methods', () => { closeM(); openNavigation(d.className); }));
    actions.appendChild(navButton('Object graph', () => { closeM(); openNavigation('', 'objects'); }));
    actions.appendChild(navButton('Debug method', () => openDebugger(id, methods, d.methodSignatures || {})));
    actions.appendChild(navButton('Clone', () => cloneInspectedObject(id)));
    if (d.className === 'Wiring') actions.appendChild(navButton('Open graph', () => { closeM(); openWiring(id); })); else {}
    if (vars.kind && vars.children && vars.owner) actions.appendChild(navButton('Open control in designer', () => openControlDesigner(id))); else {}
    if (runtime.definitionVersion) actions.appendChild(navButton('Clone and edit', () => openClone(id))); else {}
    if(!preserveCall)document.getElementById('method-filter').value='';else{}
    methods.forEach(method => {
      const button = document.createElement('button');
      button.className = 'btn';
      button.textContent = method;
      button.onclick = () => {
        if (document.getElementById('msend').disabled) return;
        document.getElementById('mselector').value = method;
        prepareArguments();
        if (/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(method)) {
          document.getElementById('mcall').requestSubmit();
        }
      };
      md.appendChild(button);
    });

    filterInspectorMethods();
    renderContextHelp(id, d, inspectorMetadata);
    updateObjectWorkspace(id, d, inspectorMetadata);
    if (!debugBusy) { await loadInspectorViews(id, d, version); } else { document.getElementById('mviews').textContent = 'Debugger: ' + runtime.debug.state + ' ' + runtime.debug.method; }
    if(version!==inspectionVersion || inspectedId!==id)return;else{}
    prepareInspectorSections(!!document.querySelector('#mviews .view-body'),preserveCall);
    document.getElementById('mbg').style.display = 'block';
    document.getElementById('modal').style.display = 'block';
    if(!preserveCall)document.getElementById('inspect-tab-'+inspectorSection).focus();else{}
  } catch(_) {}
}

async function inspectorCall(id, method, args) {
  const response = await fetch('/mcp', {
    method:'POST', headers:{'Content-Type':'application/json'},
    body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name:'call',arguments:{objectId:id,method,args}}})
  });
  const reply = await response.json();
  if (reply.error) throw Error(reply.error.message);
  if (!response.ok || reply.result.isError) throw Error((reply.result.content || []).map(c => c.text || '').join('\n'));
  return JSON.parse(reply.result.content[0].text);
}

async function renderProperties(id, properties, version) {
  if (!Array.isArray(properties)) throw Error('properties must be an array');
  const container = document.getElementById('mproperties');
  for (const property of properties) {
    if (typeof property.name !== 'string' || typeof property.getter !== 'string') throw Error('A property needs a name and getter');
    const row = document.createElement('div');
    row.className = 'marg';
    const label = document.createElement('span');
    label.className = 'ivk';
    label.textContent = property.name;
    row.appendChild(label);
    try {
      const value = await inspectorCall(id, property.getter, []);
      if (version !== inspectionVersion || inspectedId !== id) return;
      const kind = property.type === "char" ? "character" : property.type || value.kind;
      if (property.setter && ['number','string','bool','nil','objid','character'].includes(kind)) {
        propertyEditor(row, id, property, value, kind);
      } else {
        const display = document.createElement('span');
        display.textContent = fmtVal(value);
        row.appendChild(value.kind === 'objid' ? objectLink(value.objid) : display);
      }
    } catch (error) {
      row.appendChild(document.createTextNode('Error: ' + error.message));
    }
    if (version !== inspectionVersion || inspectedId !== id) return;
    container.appendChild(row);
  }
}

function propertyEditor(row, id, property, value, kind) {
  const type = document.createElement('select');
  type.hidden = true;
  const option = document.createElement('option');
  option.value = kind;
  type.appendChild(option);
  const input = document.createElement('input');
  input.setAttribute('aria-label', property.name);
  input.value = value.kind === 'string' ? value.str || '' : value.kind === 'objid' ? value.objid : value.kind === 'character' ? String.fromCodePoint(value.char) : fmtVal(value);
  input.disabled = kind === 'nil';
  const save = document.createElement('button');
  save.className = 'btn';
  save.textContent = 'Set';
  save.setAttribute('aria-label', 'Set ' + property.name);
  save.onclick = () => saveProperty(id, property, row);
  input.onkeydown = event => { if (event.key === 'Enter') { event.preventDefault(); save.click(); } };
  row.append(type, input, save);
}

async function saveProperty(id, property, row) {
  const send = document.getElementById('msend');
  if (send.disabled) return;
  send.disabled = true;
  const result = document.getElementById('mresult');
  result.textContent = 'Setting ' + property.name + '…';
  try {
    const response = await performInteraction({kind:'call',objectId:id,method:property.setter,args:[argumentValue(row)]});
    if(response.error) throw Error(response.error); else {}
    if (inspectedId !== id) return;
    await showInspect(id, true);
    renderResult(result, response);
  } catch (error) {
    if (inspectedId === id) result.textContent = 'Error: ' + error.message;
  } finally {
    send.disabled = false;
  }
}

function objectLink(id) {
  const button = document.createElement('button');
  button.className = 'objlink';
  button.textContent = '#' + id;
  button.onclick = () => { closeExamples(); document.getElementById('recording-panel').hidden=true;document.getElementById('result-shelf').hidden=true;showInspect(id); };
  return button;
}

function prepareArguments() {
  const selector = document.getElementById('mselector').value.trim();
  renderSelectorHelp(selector);
  const inferred = methodSignatures[selector];
  const returnType = ((inspectorMetadata.methods || {})[selector] || {}).returns;
  document.getElementById('msignature').textContent = returnType ? 'Returns: ' + returnType : '';
  const described = ((inspectorMetadata.methods || {})[selector] || {}).arguments || [];
  const count = inferred ? inferred.length : ((selector.match(/:/g) || []).length || (/^[^a-zA-Z0-9_]+$/.test(selector) ? 1 : 0));
  const container = document.getElementById('margs');
  container.innerHTML = '';
  for (let i = 0; i < count; i++) {
    const row = document.createElement('div');
    row.className = 'marg';
    const type = document.createElement('select');
    type.setAttribute('aria-label', 'Argument ' + (i + 1) + ' type');
    ['number', 'string', 'bool', 'nil', 'objid', 'character', 'result'].forEach(kind => {
      const option = document.createElement('option');
      option.value = kind;
      option.textContent = {bool:'Boolean',objid:'Object ID',char:'Character'}[kind] || kind;
      type.appendChild(option);
    });
    const input = document.createElement('input');
    input.setAttribute('aria-label', 'Argument ' + (i + 1));
    const argument = described[i] || {};
    const name = argument.name || (inferred || [])[i] || 'Argument ' + (i + 1);
    input.placeholder = name;
    input.setAttribute('aria-label', name);
    if ([...type.options].some(option => option.value === argument.type)) type.value = argument.type;
    input.disabled = type.value === 'nil';
    type.onchange = () => { input.disabled = type.value === 'nil'; configureResultInput(type,input); };
    row.append(type, input);
    container.appendChild(row);
  }
}

function argumentValue(row) {
  const kind = row.querySelector('select').value;
  const text = row.querySelector('input').value;
  switch (kind) {
    case 'result': return retainedArgument(text);
    case 'number':
      if (!text.trim() || !Number.isFinite(Number(text))) throw Error('Enter a valid number.');
      return {kind, num:Number(text)};
    case 'string': return {kind, str:text};
    case 'nil': return {kind};
    case 'bool':
      if (text !== 'true' && text !== 'false') throw Error('Enter true or false.');
      return {kind, bool:text === 'true'};
    case 'objid':
      if (!text.trim()) throw Error('Enter an object ID.');
      return {kind, objid:text.trim()};
    case 'character':
      if ([...text].length !== 1) throw Error('Enter one character.');
      return {kind, char:text.codePointAt(0)};
  }
}

async function sendObjectMessage(event) {
  event.preventDefault();
  const id = inspectedId;
  const result = document.getElementById('mresult');
  const button = document.getElementById('msend');
  button.disabled = true;
  result.textContent = 'Sending…';
  try {
    const method = document.getElementById('mselector').value.trim();
    if (!method) throw Error('Choose a method.');
    const args = [...document.querySelectorAll('#margs .marg')].map(argumentValue);
    const response = await performInteraction({kind:'call',objectId:id,method,args});
    if(response.error) throw Error(response.error); else {}
    if (inspectedId !== id) return;
    renderResult(result,response);
    await showInspect(id, true);
    await refreshObjs();
  } catch (error) {
    if (inspectedId === id) result.textContent = 'Error: ' + error.message;
  } finally {
    button.disabled = false;
  }
}

function fmtVal(v) {
  if (!v || v.kind === 'nil') return 'nil';
  switch(v.kind) {
    case 'number': return String(v.num ?? 0);
    case 'string': return '"' + (v.str || '') + '"';
    case 'objid':  return '#' + v.objid;
    case 'bool':   return String(v.bool ?? false);
    case 'character':   return String.fromCodePoint(v.char ?? 0);
  }
  return 'nil';
}

function closeM() {
  if(typeof workbenchObject!=='undefined'&&inspectedId===workbenchObject)loadWorkbench();else{}
  inspectedId = null;
  inspectionVersion++;
  document.getElementById('mbg').style.display = 'none';
  document.getElementById('modal').style.display = 'none';
  if(inspectorReturnFocus&&inspectorReturnFocus.isConnected)inspectorReturnFocus.focus();else{}
}

function esc(s) {
  return String(s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;')
    .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

document.addEventListener('keydown', e => { if (e.key === 'Escape') closeM(); });
</script>
<script>` + workbenchLayoutJS + `</script>
<script src="/navigation.js"></script>
<script src="/interaction.js"></script>
<script src="/views.js"></script>
<script src="/object-workspace.js"></script>
<script src="/debugger.js"></script>
<script src="/trace.js"></script>
<script src="/source-history.js"></script>
<script src="/cloning.js"></script>
<script src="/wiring.js"></script>
<script src="/promises.js"></script>
<script src="/workbench.js"></script>
<script src="/visual-editor.js"></script>
<script src="/refactoring.js"></script>
<script>` + discoveryJS + `</script>
</body>
</html>`
