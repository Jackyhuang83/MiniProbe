const $=s=>document.querySelector(s);
let dashboardMode='';

const fmtBytes=n=>{n=Number(n||0);if(!Number.isFinite(n))return'-';const u=['B','KB','MB','GB','TB','PB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return `${n.toFixed(i?1:0)} ${u[i]}`};
const fmtTraffic=n=>{n=Number(n||0);if(!Number.isFinite(n))return'-';const u=['B','KB','MB','GB','TB','PB'];let i=0;while(n>=1000&&i<u.length-1){n/=1000;i++}return `${n.toFixed(i?1:0)} ${u[i]}`};
const pct=(a,b)=>b?Math.min(100,Math.max(0,Number(a||0)/Number(b)*100)):0;
const uptime=s=>{s=Number(s||0);let d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60);return d?`${d}天 ${h}小时`:`${h}小时 ${m}分`};
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

const REGION_FLAGS={
  HK:'🇭🇰',JP:'🇯🇵',SG:'🇸🇬',US:'🇺🇸',CN:'🇨🇳',TW:'🇹🇼',KR:'🇰🇷',DE:'🇩🇪',GB:'🇬🇧',UK:'🇬🇧',
  NL:'🇳🇱',FR:'🇫🇷',CA:'🇨🇦',AU:'🇦🇺',MY:'🇲🇾',TH:'🇹🇭',VN:'🇻🇳',PH:'🇵🇭',ID:'🇮🇩',IN:'🇮🇳'
};

function regionCode(n){
  const candidates=[...(n.tags||[]),...(String(n.display_name||'').split(/[-_\s]+/))];
  for(const raw of candidates){
    const k=String(raw||'').trim().toUpperCase();
    if(REGION_FLAGS[k]) return k;
  }
  return '';
}
function regionFlag(n){const c=regionCode(n);return c?REGION_FLAGS[c]:'🌐'}

function shortVersion(raw,name){
  const m=String(raw||'').match(new RegExp(`${name}(?: GNU/Linux| Linux)?\\s*([0-9.]+)?`,'i'));
  return m?.[1]?`${name} ${m[1]}`:name;
}
function osIdentity(os){
  const raw=String(os||'Linux').trim(),s=raw.toLowerCase();
  if(s.includes('debian')) return {src:'/debian.svg',label:shortVersion(raw,'Debian')};
  if(s.includes('ubuntu')) return {src:'/ubuntu.svg',label:shortVersion(raw,'Ubuntu')};
  if(s.includes('alpine')) return {src:'/alpine.svg',label:shortVersion(raw,'Alpine')};
  if(s.includes('rocky')) return {src:'/rocky.svg',label:'Rocky Linux'};
  if(s.includes('alma')) return {src:'/alma.svg',label:'AlmaLinux'};
  return {src:'',label:raw||'Linux'};
}
function osMark(os){
  if(os.src) return `<img class="os-logo" src="${os.src}" alt="">`;
  return `<span class="os-logo fallback">L</span>`;
}

function priceText(n){
  const v=Number(n.monthly_price||0); if(!(v>0)) return '';
  const c=String(n.currency||'').toUpperCase();
  const symbols={CNY:'¥',RMB:'¥',USD:'$',HKD:'HK$',JPY:'¥',EUR:'€',GBP:'£',SGD:'S$',AUD:'A$',CAD:'C$'};
  const p=symbols[c]||c;
  return `${p}${Number.isInteger(v)?v:v.toFixed(2)}/月`;
}
function expiryInfo(v){
  if(!v) return {text:'',days:null};
  const d=new Date(v); if(Number.isNaN(d.getTime())) return {text:'',days:null};
  const days=Math.ceil((d.getTime()-Date.now())/86400000);
  if(days<0) return {text:`已到期 ${Math.abs(days)} 天`,days};
  if(days===0) return {text:'今天到期',days};
  return {text:`剩余 ${days} 天`,days};
}

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
function metricIcon(kind){return {cpu:'▣',mem:'▥',disk:'▱',traffic:'↕'}[kind]||'•'}
function metric(kind,name,used,total,detail=''){
  const p=pct(used,total),d=detail||(total?`${fmtBytes(used)} / ${fmtBytes(total)}`:'-');
  return `<div class="metric ${kind}"><div class="metric-head"><span class="metric-name"><i>${metricIcon(kind)}</i>${name}</span><strong>${total?p.toFixed(1)+'%':'-'}</strong></div><div class="bar"><div class="fill" style="width:${total?p:0}%"></div></div><div class="metric-detail">${d}</div></div>`;
}
function cpu(n){
  const m=n.metrics||{},i=n.info||{};let p=Math.max(0,Math.min(100,Number(m.cpu_percent||0)));
  return `<div class="metric cpu"><div class="metric-head"><span class="metric-name"><i>${metricIcon('cpu')}</i>CPU</span><strong>${n.online?p.toFixed(1)+'%':'-'}</strong></div><div class="bar"><div class="fill" style="width:${n.online?p:0}%"></div></div><div class="metric-detail">${i.cpu_cores?`${i.cpu_cores} 核 · 负载 ${Number(m.load1||0).toFixed(2)}`:'-'}</div></div>`;
}
function trafficMetric(n){
  const t=n.traffic||{},limit=Number(n.monthly_traffic_limit||0),used=Number(t.used_bytes||0),p=limit?pct(used,limit):0;
  if(!limit) return `<div class="metric traffic"><div class="metric-head"><span class="metric-name"><i>${metricIcon('traffic')}</i>流量</span><strong class="muted-strong">未设置</strong></div><div class="bar"><div class="fill" style="width:0"></div></div><div class="metric-detail">未设置配额</div></div>`;
  return `<div class="metric traffic"><div class="metric-head"><span class="metric-name"><i>${metricIcon('traffic')}</i>流量</span><strong>${p.toFixed(1)}%</strong></div><div class="bar"><div class="fill" style="width:${p}%"></div></div><div class="metric-detail">${fmtTraffic(used)} / ${fmtTraffic(limit)}</div></div>`;
}

function probe(p){
  if(p.available===false) return `<div class="probe unavailable"><div class="probe-top"><span class="name">${esc(p.name)}</span><span class="latency">N/A</span><span class="loss">N/A</span></div><div class="hist">${'<i class="sq"></i>'.repeat(30)}</div></div>`;
  const latency=Number(p.latency_ms),timedOut=!Number.isFinite(latency)||latency<0;
  let h=[...(p.history||[])];while(h.length<30)h.unshift(-1);
  return `<div class="probe ${timedOut?'timeout':''}"><div class="probe-top"><span class="name">${esc(p.name)}</span><span class="latency">${timedOut?'超时':latency.toFixed(0)+' ms'}</span><span class="loss">${Number(p.loss_pct||0).toFixed(1)}%</span></div><div class="hist">${h.map(v=>`<i class="sq ${v>=0?'g'+v:''}"></i>`).join('')}</div></div>`;
}

function miniBox(label,body,klass=''){
  return `<div class="mini-box ${klass}"><div class="mini-label">${label}</div><div class="mini-body">${body}</div></div>`;
}

function card(n){
  const m=n.metrics||{},i=n.info||{},t=n.traffic||{},os=osIdentity(i.os),exp=expiryInfo(n.expire_at),price=priceText(n),region=regionCode(n);
  const name=n.display_name||n.node_id;
  const tagHtml=(n.tags||[]).filter(x=>String(x).toUpperCase()!==region).map(x=>`<span class="tag">${esc(x)}</span>`).join('');
  const planBody=(exp.text||price)?`<span class="plan-primary ${exp.days!==null&&exp.days<=7?'urgent':''}">${esc(exp.text||'未设置到期')}</span><span>${esc(price||'未设置价格')}</span>`:`<span class="plan-primary">未设置</span><span>套餐信息</span>`;
  return `<article class="card ${n.online?'':'offline-card'}">
    <div class="top">
      <div class="title"><span class="dot ${n.online?'':'off'}"></span><span class="flag">${regionFlag(n)}</span><span class="title-text">${esc(name)}</span></div>
      <div class="top-right">${osMark(os)}<span class="status-text">${n.online?'在线':'离线'}</span></div>
    </div>
    <div class="chips">
      <span class="chip">${n.online?`在线 ${uptime(m.uptime_seconds)}`:'等待恢复'}</span>
      ${price?`<span class="chip">${esc(price)}</span>`:''}
      <span class="chip ghost">${esc(os.label)}${i.virtualization?` · ${esc(String(i.virtualization).toUpperCase())}`:''}${i.arch?` · ${esc(i.arch)}`:''}</span>
    </div>

    <div class="metrics-grid">
      ${cpu(n)}
      ${metric('mem','内存',m.mem_used,i.mem_total)}
      ${metric('disk','硬盘',m.disk_used,i.disk_total)}
      ${trafficMetric(n)}
    </div>

    <div class="mini-grid">
      ${miniBox('实时',`<span class="up">↑ ${fmtBytes(m.net_tx_bps)}/s</span><span class="down">↓ ${fmtBytes(m.net_rx_bps)}/s</span>`,'speed-box')}
      ${miniBox('本月',`<span>↑ ${fmtTraffic(t.outbound_bytes||0)}</span><span>↓ ${fmtTraffic(t.inbound_bytes||0)}</span>`)}
      ${miniBox('套餐',planBody,'plan-box')}
    </div>

    <div class="probe-panel">
      <div class="probe-head"><span>线路质量</span><span>延迟 · 丢包 · 最近30次</span></div>
      ${(n.probes||[]).map(probe).join('')||'<div class="small waiting">等待线路探测数据…</div>'}
    </div>

    ${tagHtml?`<div class="footer">${tagHtml}</div>`:''}
  </article>`;
}

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
