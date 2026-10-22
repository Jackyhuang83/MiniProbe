const $=s=>document.querySelector(s);
const UI_VERSION='0.4.27-alpha';
const UI_BASE=`/ui/${UI_VERSION}`;
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
  if(s.includes('debian')) return {src:`${UI_BASE}/debian.svg`,label:shortVersion(raw,'Debian')};
  if(s.includes('ubuntu')) return {src:`${UI_BASE}/ubuntu.svg`,label:shortVersion(raw,'Ubuntu')};
  if(s.includes('alpine')) return {src:`${UI_BASE}/alpine.svg`,label:shortVersion(raw,'Alpine')};
  if(s.includes('rocky')) return {src:`${UI_BASE}/rocky.svg`,label:'Rocky Linux'};
  if(s.includes('alma')) return {src:`${UI_BASE}/alma.svg`,label:'AlmaLinux'};
  return {src:'',label:raw||'Linux'};
}
function osMark(os){
  if(os.src) return `<img class="os-logo" src="${os.src}" alt="">`;
  return `<span class="os-logo fallback">L</span>`;
}
function networkBadges(types){
  return (types||[]).map(x=>String(x)).filter(x=>x==='V4'||x==='V6').map(x=>`<span class="net-badge">${esc(x)}</span>`).join('');
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
  const s=String(v).trim();
  if(s==='2036-01-01'||s.toLowerCase()==='longterm') return {text:'长期',days:null};
  const d=new Date(s); if(Number.isNaN(d.getTime())) return {text:'',days:null};
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

function updateSecurityBanner(session){
  const insecure=session?.mode!=='disabled' && session?.secure===false;
  const el=$('#securityWarning');
  if(el){
    el.classList.toggle('hidden',!insecure);
    if(insecure) el.textContent='⚠ 当前通过 HTTP 直连访问，传输未加密。建议在 Server SSH 菜单 8 启用 Cloudflare Tunnel HTTPS。';
  }
  const loginEl=$('#loginSecurityWarning');
  if(loginEl){
    loginEl.classList.toggle('hidden',!insecure);
    if(insecure) loginEl.textContent='⚠ 当前连接为 HTTP，密码传输未加密。建议通过 SSH 菜单 8 配置 HTTPS 后再长期使用。';
  }
}
const METRIC_ICONS={
  cpu:'<svg class="metric-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="5" width="14" height="14" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/></svg>',
  mem:'<svg class="metric-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="6" width="16" height="12" rx="2"/><path d="M8 9v6M12 9v6M16 9v6M7 3v3M11 3v3M15 3v3M7 18v3M11 18v3M15 18v3"/></svg>',
  disk:'<svg class="metric-icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="5" width="16" height="14" rx="2"/><path d="M7 14h10M8 9h8"/><circle cx="8" cy="16" r=".8" fill="currentColor" stroke="none"/></svg>',
  traffic:'<svg class="metric-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M8 4v15M4.5 7.5 8 4l3.5 3.5M16 20V5M12.5 16.5 16 20l3.5-3.5"/></svg>'
};
function metricIcon(kind){return METRIC_ICONS[kind]||''}
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
  if(!limit) return `<div class="metric traffic"><div class="metric-head"><span class="metric-name"><i>${metricIcon('traffic')}</i>流量</span><strong class="muted-strong">—</strong></div><div class="bar bar-empty"><div class="fill" style="width:0"></div></div><div class="metric-detail">未设置配额</div></div>`;
  return `<div class="metric traffic"><div class="metric-head"><span class="metric-name"><i>${metricIcon('traffic')}</i>流量</span><strong>${p.toFixed(1)}%</strong></div><div class="bar"><div class="fill" style="width:${p}%"></div></div><div class="metric-detail">${fmtTraffic(used)} / ${fmtTraffic(limit)}</div></div>`;
}

function stateClass(v){return Number.isInteger(v)&&v>=0&&v<=3?`s${v}`:''}
function trimHistory(values,count=20){
  let h=[...(values||[])].slice(-count);
  while(h.length<count) h.unshift(-1);
  return h;
}
function latencyState(ms){
  if(!Number.isFinite(ms)||ms<0) return 3;
  if(ms>200) return 2;
  if(ms>100) return 1;
  return 0;
}
function lossState(pct){
  const v=Number(pct||0);
  if(v>=100) return 3;
  if(v>=50) return 2;
  if(v>0) return 1;
  return 0;
}
function probeTrack(values,kind){
  return `<div class="hist ${kind}-hist">${trimHistory(values).map(v=>`<i class="sq ${stateClass(v)}"></i>`).join('')}</div>`;
}
function probe(p){
  const available=p.available!==false;
  const latency=Number(p.latency_ms),loss=Number(p.loss_pct||0);
  const timedOut=!available||!Number.isFinite(latency)||latency<0;
  const latNow=available?latencyState(latency):-1;
  const lossNow=available?lossState(loss):-1;

  let latHist=(p.latency_history||[]);
  if(!latHist.length) latHist=[...(p.history||[])];
  let lossHist=(p.loss_history||[]);
  if(!lossHist.length){
    lossHist=[...(p.history||[])].map(v=>Number(v)===3?3:0);
    if(lossHist.length) lossHist[lossHist.length-1]=lossNow;
  }

  return `<div class="probe ${available?'':'unavailable'}">
    <div class="probe-half">
      <div class="probe-line"><span class="name">${esc(p.name)}</span><span class="probe-value latency ${stateClass(latNow)}">${available?(timedOut?'超时':latency.toFixed(0)+' ms'):'N/A'}</span></div>
      ${probeTrack(available?latHist:[],'latency')}
    </div>
    <div class="probe-half">
      <div class="probe-line"><span></span><span class="probe-value loss ${stateClass(lossNow)}">${available?loss.toFixed(1)+'%':'N/A'}</span></div>
      ${probeTrack(available?lossHist:[],'loss')}
    </div>
  </div>`;
}

function miniBox(label,body,klass=''){
  return `<div class="mini-box ${klass}"><div class="mini-label">${label}</div><div class="mini-body">${body}</div></div>`;
}

function routeStatus(n){
  const s=String(n.route_status||'collecting');
  if(s==='changed') return {cls:'changed',text:`⚠ ${Number(n.route_changed_count||0)||1} 条路由变化`};
  if(s==='checking') return {cls:'checking',text:'线路变化确认中'};
  if(s==='normal') return {cls:'normal',text:'线路正常'};
  if(s==='unavailable') return {cls:'unavailable',text:'路由暂不可用'};
  return {cls:'collecting',text:'线路基准采集中'};
}


const unlockLabels={chatgpt:'ChatGPT',gemini:'Gemini',claude:'Claude',google:'Google',netflix:'Netflix',disney:'Disney+',youtube:'YouTube Premium',tiktok:'TikTok',reddit:'Reddit',mytvsuper:'MyTV SUPER'};
const unlockStates={reachable:'可访问（未验证实际播放）',limited:'部分受限',blocked:'地区不可用',unknown:'结果未知'};
const unlockGlyphs={
  reachable:'<path d="m7 12.5 3.5 3.5L17.5 8"/>',
  limited:'<path d="M7 12h10"/>',
  blocked:'<path d="m8 8 8 8m0-8-8 8"/>',
  unknown:'<path d="M9.6 9.5a2.6 2.6 0 0 1 5 1c0 1.6-2.5 2-2.5 3.7"/><path d="M12 17h.01"/>'
};
const unlockRegionAliases={JPN:'JP',CHN:'CN',HKG:'HK',MAC:'MO',TWN:'TW',USA:'US',GBR:'GB',SGP:'SG',DEU:'DE',KOR:'KR',FRA:'FR',CAN:'CA',AUS:'AU',RUS:'RU',IND:'IN',MYS:'MY',THA:'TH',IDN:'ID',VNM:'VN',PHL:'PH',NLD:'NL',CHE:'CH',ITA:'IT',ESP:'ES',BRA:'BR',TUR:'TR',ARE:'AE',IRN:'IR',CUB:'CU',PRK:'KP',SYR:'SY',BLR:'BY'};
function unlockRegionCode(value){
  const raw=String(value||'').trim().toUpperCase();
  if(!/^[A-Z]{2,3}$/.test(raw)) return '';
  return unlockRegionAliases[raw]||raw;
}
function unlockStateIcon(state){
  // Inline SVG; no dependency on remote fonts, emoji appearance or a CDN.
  return `<svg class="unlock-state-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><circle cx="12" cy="12" r="10"/><g fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">${unlockGlyphs[state]}</g></svg>`;
}

function asnRow(n){
  const i=n.identity||{}, items=[['V4',i.ipv4],['V6',i.ipv6]];
  const parts=items.filter(x=>x[1]&&x[1].number).map(([family,v])=>
    `<span class="asn-entry"><span class="asn-family">${family}</span><b>${esc(v.number)}</b><span class="asn-company" title="${esc(v.org||'公司未知')}">${esc(v.org||'公司待识别')}</span></span>`);
  return `<div class="asn-row"><span class="panel-label">出口 ASN</span>${parts.join('')||'<span class="asn-missing">等待采集</span>'}</div>`;
}
// The dashboard stays read-only: activation is allowed only over the
// root-owned Unix socket through SSH menu 14.
const openIPAttributeCards=new Set();
function ipAttributeLine(label,data){
  const value=data||{};
  const checked=value.checked_at?new Date(value.checked_at).toLocaleString():'';
  const kind={idc:'IDC · 数据中心',mobile:'移动网络',non_idc:'非 IDC · 家宽待核实',unknown:'未知'}[value.class]||'未知';
  const native='未知（服务商数据无法确认原生）';
  const signals=[value.vpn?'VPN':'',value.proxy?'代理':'',value.tor?'Tor':''].filter(Boolean).join(' / ')||'未报告已知代理风险';
  return `<div class="ip-attr-row"><b>${esc(label)}</b><div><span class="ip-attr-type">${esc(kind)}</span><span class="ip-attr-country">${esc(value.country||'')}</span><span class="ip-attr-detail">原生：${native} · ${value.state==='ok'?esc(signals):'暂无有效结果'}</span>${checked?`<span class="ip-attr-date">查询：${esc(checked)}（UTC 日限额）</span>`:'<span class="ip-attr-date">等待下一次出口 IP 采集</span>'}</div></div>`;
}
function ipAttributePanel(n){
  const enabled=n.ip_attributes_enabled===true;
  const attributes=n.ip_attributes||{};
  const expanded=openIPAttributeCards.has(String(n.node_id));
  return `<details class="ip-attr-panel" data-ip-node="${esc(n.node_id||'')}"${expanded?' open':''}>
    <summary>IP 属性 · ${enabled?'参考检测':'未启用'}<span class="ip-attr-chevron" aria-hidden="true">⌄</span></summary>
    <div class="ip-attr-inner">
      ${enabled?`${ipAttributeLine('IPv4',attributes.ipv4)}${ipAttributeLine('IPv6',attributes.ipv6)}
      <div class="ip-attr-note">第三方 ipquery.io 数据，仅供参考；非 IDC 不等于家宽，原生无法可靠认证。查询每天每节点每地址族最多 1 次。</div>`:
      '<div class="ip-attr-note">默认关闭。需要 SSH 登录主 Server，在 miniprobe 菜单 14 主动启用；网页仅展示。</div>'}
    </div>
  </details>`;
}

function unlockRow(n){
  const items=Array.isArray(n.unlock)?n.unlock:[];
  if(!items.length) return '<div class="unlock-panel"><span class="panel-label">平台检测</span><span class="asn-missing">等待 Agent 检测</span></div>';
  const chips=items.filter(x=>Object.prototype.hasOwnProperty.call(unlockLabels,x.service)).map(x=>{
    const state=Object.prototype.hasOwnProperty.call(unlockStates,x.status)?x.status:'unknown';
    const region=unlockRegionCode(x.region);
    const fromEgress=region&&x.region_source==='egress';
    const checked=x.checked_at?new Date(x.checked_at).toLocaleString():'';
    const label=unlockLabels[x.service];
    const detail=unlockStates[state];
    const regionNote=region?(fromEgress?` · 出口地区参考 ≈${region}，非平台解锁地区`:` · 平台请求地区 ${region}（非实际播放验证）`):' · 未取得地区信息';
    const title=`${label} · ${detail}${regionNote}${checked?' · '+checked:''}`;
    return `<span class="unlock-chip ${state}" title="${esc(title)}" aria-label="${esc(title)}">${unlockStateIcon(state)}<span class="unlock-name">${esc(label)}</span>${region?`<span class="unlock-region${fromEgress?' egress':''}">${fromEgress?'≈':''}${esc(region)}</span>`:''}</span>`;
  }).join('');
  const estimated=items.some(x=>x.region_source==='egress'&&unlockRegionCode(x.region));
  return `<div class="unlock-panel"><div class="panel-label">平台检测 · ${chips?items.length:0} 项${estimated?'<span class="unlock-region-hint" title="≈ 是 VPS 默认出口地区参考，不是该平台的解锁国家"> · ≈出口参考</span>':''}</div><div class="unlock-chips">${chips}</div></div>`;
}

function card(n){
  const m=n.metrics||{},i=n.info||{},t=n.traffic||{},os=osIdentity(i.os),exp=expiryInfo(n.expire_at),price=priceText(n),region=regionCode(n);
  const name=n.display_name||n.node_id,route=routeStatus(n);
  const probeProtocol=String(n.probe_protocol||'').toUpperCase();
  const tagHtml=(n.tags||[]).filter(x=>String(x).toUpperCase()!==region).map(x=>`<span class="tag">${esc(x)}</span>`).join('');
  const planBody=(exp.text||price)?`<span class="plan-primary ${exp.days!==null&&exp.days<=7?'urgent':''}">${esc(exp.text||'未设置到期')}</span><span>${esc(price||'未设置价格')}</span>`:`<span class="plan-primary">未设置</span>`;
  return `<article class="card ${n.online?'':'offline-card'}">
    <div class="top">
      <div class="title"><span class="flag">${regionFlag(n)}</span><span class="title-text">${esc(name)}</span></div>
      <div class="top-right"><span class="dot ${n.online?'':'off'}"></span><span class="status-text">${n.online?'在线':'离线'}</span></div>
    </div>
    <div class="chips">
      <span class="chip">${n.online?`在线 ${uptime(m.uptime_seconds)}`:'等待恢复'}</span>
      <span class="chip ghost os-chip">${osMark(os)}<span class="os-text">${esc(os.label)}${i.virtualization?` · ${esc(String(i.virtualization).toUpperCase())}`:''}${i.arch?` · ${esc(i.arch)}`:''}</span>${networkBadges(i.network_types)}</span>
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
      ${probeProtocol&&probeProtocol!=='OFF'?`<span class="probe-protocol">${esc(probeProtocol)}</span>`:''}
      ${probeProtocol==='OFF'?'<div class="small waiting">线路测试已关闭</div>':((n.probes||[]).map(probe).join('')||'<div class="small waiting">等待线路探测数据…</div>')}
    </div>

    ${asnRow(n)}
    ${ipAttributePanel(n)}
    ${unlockRow(n)}
    <div class="route-strip"><span class="route-state ${route.cls}">${route.text}</span><a class="route-link" href="${UI_BASE}/network.html?id=${encodeURIComponent(n.node_id)}">查看线路详情 ›</a></div>
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

$('#grid').addEventListener('toggle',event=>{
  const el=event.target;
  if(!el.matches || !el.matches('.ip-attr-panel')) return;
  const key=el.getAttribute('data-ip-node');
  if(!key)return;
  if(el.open)openIPAttributeCards.add(key);
  else openIPAttributeCards.delete(key);
},true);

async function boot(){
  try{
    const s=await api('/api/v1/dashboard/session');
    const liveVersion=String(s.server_version||'').trim();
    if(liveVersion && liveVersion!==UI_VERSION){
      const u=new URL(location.href);
      const q=u.searchParams.toString();
      location.replace(`/ui/${encodeURIComponent(liveVersion)}/${q?`?${q}`:''}`);
      return;
    }
    dashboardMode=s.mode;
    updateSecurityBanner(s);
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
    const result=await api('/api/v1/dashboard/login',{method:'POST',body:{password:$('#loginPass').value}});
    updateSecurityBanner({mode:'protected',secure:result?.secure===true});
    $('#loginPass').value='';show('appView');$('#logoutBtn').classList.remove('hidden');await refresh();
  }catch(err){$('#loginError').textContent=err.status===429?'尝试次数过多，请稍后再试。':'密码错误'}
});

$('#logoutBtn').addEventListener('click',async()=>{
  try{await api('/api/v1/dashboard/logout',{method:'POST'})}catch{}
  show('loginView');
});

boot();
