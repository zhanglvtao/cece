package observatory

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Cece Agent Observatory</title>
<style>
:root{background:#050505;color:#ddd;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color-scheme:dark}
body{margin:0;padding:18px;background:#050505;color:#ddd}.shell{border:1px solid #444;border-radius:10px;background:#070707;min-height:calc(100vh - 38px);padding:14px;box-sizing:border-box}.top{display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid #333;padding-bottom:10px;margin-bottom:18px}.title{font-size:18px;color:#f5f5f5;letter-spacing:.04em}.meta{color:#999;font-size:12px}.live{color:#7ee787}.grid{display:grid;grid-template-columns:1fr 360px;gap:18px}.canvas{position:relative;height:450px;border:1px solid #262626;border-radius:8px;background:linear-gradient(180deg,#0d0d0d,#080808);overflow:hidden}.node{position:absolute;border:1px solid #555;border-radius:8px;padding:8px 10px;background:#111;color:#ddd;min-width:96px;text-align:center}.node small{display:block;color:#888;margin-top:3px;font-size:10px}.active{border-color:#7ee787;box-shadow:0 0 18px rgba(126,231,135,.2)}.waiting{border-color:#d29922;box-shadow:0 0 18px rgba(210,153,34,.15)}.done{border-color:#6e7681;color:#aaa}.failed{border-color:#f85149;color:#f85149}.idle{border-color:#444;color:#888}.edge{position:absolute;color:#777;font-size:12px;white-space:pre}.edge.active{color:#7ee787;text-shadow:0 0 8px rgba(126,231,135,.35);border:0;box-shadow:none}.edge.waiting{color:#d29922;text-shadow:0 0 8px rgba(210,153,34,.25);border:0;box-shadow:none}.panel{border:1px solid #262626;border-radius:8px;padding:12px;background:#0b0b0b}.label{color:#999;margin-bottom:10px}.kv{display:grid;grid-template-columns:120px 1fr;gap:7px;font-size:12px}.k{color:#777}.evidence{font-size:12px;line-height:1.65;color:#aaa}.time{color:#666}.chip{display:inline-block;border:1px solid #333;border-radius:999px;padding:5px 9px;margin:0 6px 6px 0;color:#999;background:#101010;font-size:12px}.chip.done{border-color:#555;color:#aaa}.chip.active{border-color:#7ee787;color:#7ee787}.chip.waiting{border-color:#d29922;color:#d29922}.chip.failed{border-color:#f85149;color:#f85149}
</style>
</head>
<body>
<div class="shell">
  <div class="top"><div class="title">Cece Agent Observatory</div><div class="meta"><span id="url"></span> · <span class="live">live ●</span> · <span id="last">waiting</span></div></div>
  <div class="grid">
    <div>
      <div class="canvas" id="canvas"></div>
      <div class="panel" style="margin-top:16px"><div class="label">Phase rail</div><div id="phases"></div></div>
    </div>
    <div style="display:flex;flex-direction:column;gap:14px">
      <div class="panel"><div class="label">Inspector</div><div class="kv" id="inspector"></div></div>
      <div class="panel" style="min-height:300px"><div class="label">Evidence</div><div class="evidence" id="evidence"></div></div>
      <div class="panel" style="color:#777;font-size:12px">Data path: protocol.Event / protocol.Action → Observatory Hub → SSE → Web topology</div>
    </div>
  </div>
</div>
<script>
const slots={
  user:[36,36],tui:[210,36],runtime:[410,36],hub:[52,330],engine:[410,178],model:[640,178],orchestrator:[640,330]
};
const edgeSlots={
  'user->tui:message':[130,60,'──message──▶'],
  'tui->runtime:input action':[325,60,'──input action──▶'],
  'runtime->hub:events':[270,238,'◀──events──'],
  'hub->engine:all events':[190,350,'──all events──▶'],
  'engine->model:request':[525,200,'──request──▶'],
  'model->engine:stream':[548,242,'◀──stream delta──'],
  'engine->orchestrator:spawn':[540,350,'──spawn──▶']
};
function cls(status){return status==='active'?'active':status==='waiting'?'waiting':status==='done'?'done':status==='failed'?'failed':'idle'}
function esc(s){return String(s||'').replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]))}
function edgeKey(e){return e.from+'->'+e.to+':'+(e.label||'')}
function render(state){
  document.getElementById('url').textContent=state.server&&state.server.url?state.server.url:location.origin;
  document.getElementById('last').textContent=state.updated_at?('last '+new Date(state.updated_at).toLocaleTimeString()):'waiting';
  const canvas=document.getElementById('canvas');canvas.innerHTML='';
  const nodes=(state.nodes||[]).slice();
  let agentY=270;
  for(const n of nodes){
    let p=slots[n.id];
    if(!p&&n.kind==='tool')p=[410,330];
    if(!p&&n.kind==='agent'){p=[820,agentY];agentY+=86;}
    if(!p)continue;
    const el=document.createElement('div');el.className='node '+cls(n.status);el.style.left=p[0]+'px';el.style.top=p[1]+'px';
    const meta=n.meta&&Object.keys(n.meta).length?Object.values(n.meta)[0]:n.status;
    el.innerHTML=esc(n.label||n.id)+'<small>'+esc(meta||'')+'</small>';canvas.appendChild(el);
  }
  for(const e of state.edges||[]){
    const k=edgeKey(e);let p=edgeSlots[k];
    if(!p&&e.to&&e.to.startsWith('tool:'))p=[450,260,'│\n│ tool exec\n▼'];
    if(!p&&e.from==='orchestrator')p=[750,e.status==='waiting'?390:300,'──▶'];
    if(!p)continue;
    const el=document.createElement('div');el.className='edge '+cls(e.status);el.style.left=p[0]+'px';el.style.top=p[1]+'px';el.textContent=p[2];canvas.appendChild(el);
  }
  document.getElementById('phases').innerHTML=(state.phases||[]).map(p=>'<span class="chip '+cls(p.status)+'">'+esc(p.label)+' '+mark(p.status)+'</span>').join('');
  const activeNode=nodes.find(n=>n.status==='active')||nodes.find(n=>n.status==='waiting')||{};
  const activeEdge=(state.edges||[]).find(e=>e.status==='active')||(state.edges||[]).find(e=>e.status==='waiting')||{};
  const metrics={};(state.metrics||[]).forEach(m=>metrics[m.name]=m.value);
  document.getElementById('inspector').innerHTML=kv('active node',activeNode.label||'')+kv('active edge',edgeText(activeEdge))+kv('phase',activePhase(state.phases||[]))+kv('model',metrics.model||'')+kv('tokens',[metrics.input_tokens,metrics.context_window].filter(Boolean).join(' / '))+kv('subscribers',state.subscribers||0);
  const ev=(state.evidence||[]).slice(-12).reverse();
  document.getElementById('evidence').innerHTML=ev.map(e=>'<div><span class="time">'+new Date(e.time).toLocaleTimeString()+'</span> '+esc(e.text)+'</div>').join('');
}
function kv(k,v){return '<div class="k">'+esc(k)+'</div><div>'+esc(v)+'</div>'}
function edgeText(e){return e.from?e.from+' → '+e.to:''}
function activePhase(ps){const p=ps.find(p=>p.status==='active')||ps.find(p=>p.status==='waiting');return p?p.label:''}
function mark(s){return s==='active'?'●':s==='waiting'?'◐':s==='done'?'✓':s==='failed'?'✗':'○'}
async function load(){const r=await fetch('/api/state');render(await r.json())}
load();
const es=new EventSource('/api/events');
es.onmessage=e=>{try{render(JSON.parse(e.data).state)}catch(_){}};
es.addEventListener('event',e=>{try{render(JSON.parse(e.data).state)}catch(_){}});
es.addEventListener('state',e=>{try{render(JSON.parse(e.data).state)}catch(_){}});
</script>
</body>
</html>`
