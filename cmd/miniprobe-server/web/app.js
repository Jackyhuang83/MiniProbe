const $=s=>document.querySelector(s);
let dashboardMode='';

const fmtBytes=n=>{n=Number(n||0);if(!Number.isFinite(n))return'-';const u=['B','KB','MB','GB','TB','PB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return `${n.toFixed(i?1:0)} ${u[i]}`};
const fmtTraffic=n=>{n=Number(n||0);if(!Number.isFinite(n))return'-';const u=['B','KB','MB','GB','TB','PB'];let i=0;while(n>=1000&&i<u.length-1){n/=1000;i++}return `${n.toFixed(i?1:0)} ${u[i]}`};
const pct=(a,b)=>b?Math.min(100,Number(a||0)/Number(b)*100):0;
const uptime=s=>{s=Number(s||0);let d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60);return d?`${d}天 ${h}小时`:`${h}小时 ${m}分`};
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

async function api(url, opts={}){
  const o={credentials:'same-origin',...opts,headers:{...(opts.headers||{})}};
  if(o.method && !['GET','HEAD'].includes(o.method.toUpperCase())) o.headers['X-MiniProbe-Request']='1';
  if(o.body && typeof o.body!=='string'){o.headers['Content-Type']='application/json';o.body=JSON.stringify(o.body)}
  const r=await fetch(url,o);
  let data=null;const ct=r.headers.get('content-type')||'';
  if(ct.includes('application/json')){try{data=await r.json()}catch{}}
  else {try{data=await r.text()}catch{}}
  if(!r.ok){const e=new Error(typeof data==='string'&&data.trim()?data.trim():(data?.error||`HTTP ${r.status}`));e.status=r.status;throw e}
  return data;
}

function show(id){['loginView','appView','disabledView'].forEach(x=>$('#'+x).classList.toggle('hidden',x!==id))}
function metric(name,used,total){let p=pct(used,total);return `<div><div class="metric-head"><span>${name}</span><span>${total?p.toFixed(1)+'%':'-'}</span></div><div class="bar"><div class="fill" style="width:${total?p:0}%"></div></div><div class="small" style="margin-top:4px">${total?`${fmtBytes(used)} / ${fmtBytes(total)}`:'-'}</div></div>`}
function cpu(n){const m=n.metrics||{},i=n.info||{};let p=Math.max(0,Math.min(100,Number(m.cpu_percent||0)));return `<div><div class="metric-head"><span>CPU</span><span>${n.last_seen?p.toFixed(1)+'%':'-'}</span></div><div class="bar"><div class="fill" style="width:${n.last_seen?p:0}%"></div></div><div class="small" style="margin-top:4px">${i.cpu_cores?`${i.cpu_cores} C · Load ${Number(m.load1||0).toFixed(2)}`:'-'}</div></div>`}
function probe(p){if(p.available===false)return `<div class="probe"><div class="name">${esc(p.name)}</div><div>N/A</div><div class="loss">N/A</div><div class="hist">${'<i class="sq"></i>'.repeat(30)}</div></div>`;let h=[...(p.history||[])];while(h.length<30)h.unshift(-1);return `<div class="probe"><div class="name">${esc(p.name)}</div><div>${Number(p.latency_ms)>=0?Number(p.latency_ms).toFixed(0)+' ms':'N/A'}</div><div class="loss">${Number(p.loss_pct||0).toFixed(1)}%</div><div class="hist">${h.map(v=>`<i class="sq ${v>=0?'g'+v:''}"></i>`).join('')}</div></div>`}
function card(n){const m=n.metrics||{},i=n.info||{},t=n.traffic||{};let used=Number(t.used_bytes||0);let name=n.display_name||n.node_id;let meta=n.last_seen?`在线 ${uptime(m.uptime_seconds)} · ${esc(i.os||'Linux')} · ${esc(n.observed_ip||'')}`:'等待 Agent 上线';let quota=n.monthly_traffic_limit?`${fmtTraffic(used)} / ${fmtTraffic(n.monthly_traffic_limit)}`:'未设置配额';return `<article class="card"><div class="top"><div class="title"><i class="dot ${n.online?'':'off'}"></i><span class="title-text">${esc(name)}</span></div><span class="small">${n.online?'在线':'离线'}</span></div><div class="meta">${meta}</div><div class="pair">${cpu(n)}${metric('内存',m.mem_used,i.mem_total)}</div><div class="pair">${metric('硬盘',m.disk_used,i.disk_total)}${metric('流量',used,n.monthly_traffic_limit||0)}</div><div class="traffic"><span>↑ ${fmtBytes(m.net_tx_bps)}/s</span><span>↓ ${fmtBytes(m.net_rx_bps)}/s</span></div><div class="traffic"><span>出网 ${fmtTraffic(t.outbound_bytes||0)}</span><span>入网 ${fmtTraffic(t.inbound_bytes||0)}</span></div><div class="quota-line">${quota}${n.monthly_traffic_limit?` · ${Number(t.usage_percent||0).toFixed(1)}%`:''}</div><div class="section">${(n.probes||[]).map(probe).join('')||'<div class="small">等待线路探测数据…</div>'}</div><div class="footer">${(n.tags||[]).map(t=>`<span class="tag">${esc(t)}</span>`).join('')}${i.arch?`<span class="tag">${esc(i.arch)}</span>`:''}${i.virtualization?`<span class="tag">${esc(i.virtualization)}</span>`:''}</div></article>`}

async function refresh(){
  try{
    const a=await api('/api/v1/nodes');
    $('#summary').textContent=`节点 ${a.length} · 在线 ${a.filter(x=>x.online).length}`;
    const over=a.length>15; $('#nodeWarning').classList.toggle('hidden',!over); if(over) $('#nodeWarning').textContent=`⚠ MiniProbe 为个人小规模监控设计，建议不超过 15 个节点。当前：${a.length} 个。`;
    $('#grid').innerHTML=a.length?a.map(card).join(''):'<div class="empty">还没有节点。请 SSH 登录服务器执行 miniprobe 添加节点。</div>';
  }catch(e){
    if(e.status===401){show('loginView');return}
    $('#summary').textContent='连接 Server 失败';
  }
}

async function boot(){
  try{
    const s=await api('/api/v1/dashboard/session');
    dashboardMode=s.mode;
    if(s.mode==='disabled'){show('disabledView');return}
    if(s.mode==='protected'&&!s.authenticated){show('loginView');return}
    show('appView');
    $('#logoutBtn').classList.toggle('hidden',s.mode!=='protected');
    await refresh();
    setInterval(()=>{if(!$('#appView').classList.contains('hidden'))refresh()},2500);
  }catch{
    show('disabledView');
  }
}

$('#loginForm').addEventListener('submit',async e=>{
  e.preventDefault();$('#loginError').textContent='';
  try{
    await api('/api/v1/dashboard/login',{method:'POST',body:{password:$('#loginPass').value}});
    $('#loginPass').value='';show('appView');$('#logoutBtn').classList.remove('hidden');await refresh();
  }catch(err){$('#loginError').textContent=err.status===429?'尝试次数过多，请稍后再试。':'密码错误'}
});

$('#logoutBtn').addEventListener('click',async()=>{
  try{await api('/api/v1/dashboard/logout',{method:'POST'})}catch{}
  show('loginView');
});

boot();
