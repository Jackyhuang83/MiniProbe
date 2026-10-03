const $=s=>document.querySelector(s);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
let currentData=null,currentRange='day';

async function api(url){
  const r=await fetch(url,{credentials:'same-origin'});
  if(!r.ok){const e=new Error((await r.text()).trim()||`HTTP ${r.status}`);e.status=r.status;throw e}
  return r.json();
}
function fmtTime(v){if(!v)return'—';const d=new Date(v);return Number.isNaN(d.getTime())?'—':d.toLocaleString([], {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'})}
function statusInfo(s,count=0){
  if(s==='changed')return{cls:'changed',text:`⚠ ${count||1} 条路由变化`};
  if(s==='checking')return{cls:'checking',text:'变化确认中'};
  if(s==='normal')return{cls:'normal',text:'线路正常'};
  if(s==='unavailable')return{cls:'unavailable',text:'路由暂不可用'};
  return{cls:'collecting',text:'线路基准采集中'};
}
function routePath(path){return (path||[]).length?(path||[]).map(x=>`<span class="as-chip">${esc(x)}</span>`).join('<span class="as-arrow">→</span>'):'<span class="muted-path">等待 ASN 路径…</span>'}
function normalizeASN(v){return String(v||'').trim().toUpperCase().split('/')[0]}
function routeLine(name,path){
  const set=new Set((path||[]).map(normalizeASN));
  const telecom=String(name||'').endsWith('电信'),unicom=String(name||'').endsWith('联通'),mobile=String(name||'').endsWith('移动');
  if(telecom){
    if(set.has('AS4809'))return{key:'cn2',name:'CN2',detail:'AS4809'};
    if(set.has('AS4134'))return{key:'standard',name:'163 / ChinaNet',detail:'AS4134'};
    if(set.has('AS23764'))return{key:'intl',name:'CTGNet',detail:'AS23764'};
  }
  if(unicom){
    if(set.has('AS9929'))return{key:'premium',name:'CUII / 9929',detail:'AS9929'};
    if(set.has('AS4837'))return{key:'standard',name:'4837 / China169',detail:'AS4837'};
    if(set.has('AS10099'))return{key:'intl',name:'CUG',detail:'AS10099'};
  }
  if(mobile){
    if(set.has('AS58807'))return{key:'cmin2',name:'CMIN2',detail:'AS58807'};
    if(set.has('AS58453'))return{key:'intl',name:'CMI',detail:'AS58453'};
    if(set.has('AS9808'))return{key:'standard',name:'CMNET',detail:'AS9808'};
  }
  return{key:'unknown',name:(path||[]).length?'其他 / 未识别':'等待识别',detail:''};
}
function lineBadge(line,prefix=''){
  return `<span class="line-badge ${line.key}">${prefix?`<small>${esc(prefix)}</small>`:''}<strong>${esc(line.name)}</strong>${line.detail?`<em>${esc(line.detail)}</em>`:''}</span>`;
}
function routeEventSummary(e){
  const from=routeLine(e.name,e.from),to=routeLine(e.name,e.to);
  if(from.name===to.name)return `${from.name} · ASN 路径变化`;
  return `${from.name} → ${to.name}`;
}
function carrierStatus(c){
  if(!c?.available)return{cls:'unavailable',text:'不可用'};
  if(c.status==='changed')return{cls:'changed',text:'路径变化'};
  if(c.status==='checking')return{cls:'checking',text:'确认中'};
  if(c.status==='normal')return{cls:'normal',text:'正常'};
  return{cls:'collecting',text:'采集中'};
}
function seriesCard(s,bucket){
  const loss=Number(s.loss_pct||0),cur=Number(s.current_latency_ms);
  const latency=Number.isFinite(cur)&&cur>=0?`${cur.toFixed(0)} ms`:'—';
  const points=(s.points||[]).length;
  return `<article class="series-card">
    <div class="series-head"><div><div class="series-name">${esc(s.name)}</div><div class="series-target">${esc(s.target||'')}</div></div><div class="series-now">${latency}</div></div>
    <div class="series-sub">失败率 ${loss.toFixed(2)}% (${Number(s.lost||0)}/${Number(s.sent||0)}) · ${points} 个时间点</div>
    <div class="chart-wrap"><canvas class="line-chart" data-series="${safeID(s.name+s.target)}"></canvas><div class="chart-tip hidden"></div></div>
    <div class="chart-axis"><span id="axisStart-${safeID(s.name+s.target)}"></span><span id="axisEnd-${safeID(s.name+s.target)}"></span></div>
  </article>`;
}
function safeID(s){return Array.from(String(s)).map(c=>c.codePointAt(0).toString(16)).join('').slice(0,80)}
function renderSeries(data){
  const grid=$('#seriesGrid'),series=data.series||[];
  grid.innerHTML=series.length?series.map(s=>seriesCard(s,data.bucket_seconds)).join(''):'<div class="empty">线路历史正在采集。升级 Agent 后约 1 分钟开始出现数据。</div>';
  requestAnimationFrame(()=>series.forEach(s=>{
    const key=safeID(s.name+s.target);
    const canvas=[...document.querySelectorAll('.line-chart')].find(x=>x.dataset.series===key);
    if(canvas)drawChart(canvas,s.points||[],Number(data.bucket_seconds||60),(data.route?.events||[]).filter(e=>e.name===s.name));
    const start=$('#axisStart-'+safeID(s.name+s.target)),end=$('#axisEnd-'+safeID(s.name+s.target));
    if(start)start.textContent=(s.points||[]).length?fmtShortTime(s.points[0].at):'';
    if(end)end.textContent=(s.points||[]).length?fmtShortTime(s.points[s.points.length-1].at):'';
  }));
}
function fmtShortTime(sec){const d=new Date(Number(sec)*1000);return currentRange==='day'?d.toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'}):d.toLocaleDateString([],{month:'2-digit',day:'2-digit'})}
function drawChart(canvas,points,bucket,events=[]){
  const rect=canvas.getBoundingClientRect(),dpr=Math.max(1,window.devicePixelRatio||1),w=Math.max(280,rect.width),h=Math.max(155,rect.height);
  canvas.width=Math.round(w*dpr);canvas.height=Math.round(h*dpr);const c=canvas.getContext('2d');c.scale(dpr,dpr);c.clearRect(0,0,w,h);
  const pad={l:42,r:10,t:14,b:12},valid=points.filter(p=>Number(p.latency_ms)>=0);
  c.font='10px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif';c.lineWidth=1;
  if(!valid.length){c.fillStyle='#788596';c.textAlign='center';c.fillText('数据采集中',w/2,h/2);return}
  let min=Math.min(...valid.map(p=>Number(p.latency_ms))),max=Math.max(...valid.map(p=>Number(p.latency_ms)));
  const spread=Math.max(10,max-min),margin=Math.max(5,spread*.18);min=Math.max(0,Math.floor(min-margin));max=Math.ceil(max+margin);if(max<=min)max=min+10;
  const t0=Number(points[0].at),t1=Math.max(t0+bucket,Number(points[points.length-1].at));
  const X=t=>pad.l+(Number(t)-t0)/(t1-t0)*(w-pad.l-pad.r),Y=v=>pad.t+(max-Number(v))/(max-min)*(h-pad.t-pad.b);
  c.strokeStyle='rgba(255,255,255,.09)';c.fillStyle='#7f8a99';c.textAlign='right';c.textBaseline='middle';
  for(let i=0;i<3;i++){const y=pad.t+i*(h-pad.t-pad.b)/2,val=max-i*(max-min)/2;c.beginPath();c.moveTo(pad.l,y);c.lineTo(w-pad.r,y);c.stroke();c.fillText(`${Math.round(val)}`,pad.l-7,y)}
  for(const e of events){const ts=new Date(e.at).getTime()/1000;if(!(ts>=t0&&ts<=t1))continue;const x=X(ts);c.save();c.setLineDash([4,3]);c.strokeStyle=e.type==='recovered'?'rgba(69,199,151,.65)':'rgba(239,138,60,.72)';c.beginPath();c.moveTo(x,pad.t);c.lineTo(x,h-pad.b);c.stroke();c.restore()}
  c.strokeStyle='#9bd34b';c.lineWidth=1.35;c.beginPath();let prev=null,drawing=false;
  for(const p of points){const v=Number(p.latency_ms),t=Number(p.at);if(!(v>=0)){drawing=false;prev=null;continue}const x=X(t),y=Y(v);const gap=prev? t-prev.at:0;if(!drawing||gap>bucket*2.5){c.moveTo(x,y);drawing=true}else c.lineTo(x,y);prev={at:t,x,y,v}}
  c.stroke();
  const wrap=canvas.parentElement,tip=wrap.querySelector('.chart-tip');
  canvas.onpointermove=e=>{const r=canvas.getBoundingClientRect(),px=e.clientX-r.left,ts=t0+(px-pad.l)/(w-pad.l-pad.r)*(t1-t0);let best=null,dist=Infinity;for(const p of points){if(Number(p.latency_ms)<0)continue;const d=Math.abs(Number(p.at)-ts);if(d<dist){dist=d;best=p}}if(!best){tip.classList.add('hidden');return}tip.classList.remove('hidden');tip.textContent=`${fmtTime(Number(best.at)*1000)} · ${Number(best.latency_ms).toFixed(0)} ms · 失败 ${Number(best.loss_pct||0).toFixed(1)}%`;const left=Math.min(Math.max(8,px),Math.max(8,w-185));tip.style.left=`${left}px`;tip.style.top='6px'};
  canvas.onpointerleave=()=>tip.classList.add('hidden');
}
function renderRoutes(route){
  const overall=statusInfo(route?.status,route?.changed_count);const badge=$('#networkStatus');badge.className=`route-state ${overall.cls}`;badge.textContent=overall.text;
  $('#routeMeta').textContent=`约每 30 分钟检测 · 最后检测 ${fmtTime(route?.last_checked)} · 路径变化需连续两次确认`;
  $('#asnSource').textContent=route?.asn_source?`ASN 映射：${route.asn_source}。Dashboard 不展示 traceroute 跳点 IP。`:'';
  const carriers=route?.carriers||[];$('#routeGrid').innerHTML=carriers.length?carriers.map(c=>{const st=carrierStatus(c),currentLine=routeLine(c.name,c.current),baselineLine=routeLine(c.name,c.baseline);return `<article class="route-card">
    <div class="route-card-head"><div><div class="route-card-name">${esc(c.name)}</div><div class="series-target">${esc(c.target||'')}</div></div><span class="route-state ${st.cls}">${st.text}</span></div>
    <div class="line-identify"><span class="route-label-inline">线路识别</span>${lineBadge(currentLine,'当前')}${baselineLine.name!==currentLine.name?lineBadge(baselineLine,'基准'):''}</div>
    <div class="route-label">当前 ASN 路径</div><div class="as-path">${routePath(c.current)}</div>
    <div class="route-label">基准 ASN 路径</div><div class="as-path baseline-path">${routePath(c.baseline)}</div>
    <div class="route-meta">${c.first_different?`首个不同 ASN：第 ${c.first_different} 个 · `:''}最后检测 ${fmtTime(c.last_checked)}${c.changed_at?` · 变化确认 ${fmtTime(c.changed_at)}`:''}</div>
    <details class="route-hops"><summary>查看 ASN 跳点</summary><div class="hop-list">${(c.hops||[]).length?(c.hops||[]).map(h=>`<div><span>#${Number(h.hop)}</span><strong>${esc(h.asn||'未识别')}</strong></div>`).join(''):'<div class="small">未取得可识别跳点。</div>'}</div></details>
  </article>`}).join(''):'<div class="empty route-empty">等待 Agent 首次路由采集，通常升级后数十秒内开始出现。</div>';
  const events=[...(route?.events||[])].reverse();
  $('#routeEvents').innerHTML=events.length?`<div class="route-events-title">最近路由事件</div><div class="route-event-list">${events.map(e=>`<div class="route-event"><span>${fmtTime(e.at)}</span><strong>${esc(e.name)}</strong><em class="${e.type==='recovered'?'normal':'changed'}">${e.type==='recovered'?'恢复基准':'确认变化'}</em><span class="route-event-line">${esc(routeEventSummary(e))}</span><span class="route-event-path">${esc((e.from||[]).join(' → '))} → ${esc((e.to||[]).join(' → '))}</span></div>`).join('')}</div>`:'';
}
async function load(range){
  currentRange=range;document.querySelectorAll('.range-btn').forEach(b=>b.classList.toggle('active',b.dataset.range===range));
  const id=new URLSearchParams(location.search).get('id')||'';
  if(!id){showError('缺少节点 ID。');return}
  try{
    const data=await api(`/api/v1/network/history?id=${encodeURIComponent(id)}&range=${encodeURIComponent(range)}`);currentData=data;
    $('#nodeName').textContent=data.display_name||data.node_id;document.title=`${data.display_name||'MiniProbe'} · 线路详情`;
    const hints={day:'1 分钟粒度 · 最近 24 小时',week:'5 分钟聚合 · 最近 7 天',month:'30 分钟聚合 · 最近 30 天'};$('#rangeHint').textContent=`${hints[range]} · 原始线路历史最多保留 ${data.history_retention_days||31} 天`;
    renderSeries(data);renderRoutes(data.route);$('#networkLoading').classList.add('hidden');$('#networkView').classList.remove('hidden');
  }catch(e){if(e.status===401){location.href='/';return}showError(e.message||'读取线路历史失败')}
}
function showError(msg){$('#networkLoading').classList.add('hidden');$('#networkView').classList.remove('hidden');const e=$('#networkError');e.textContent=msg;e.classList.remove('hidden')}
async function boot(){
  try{const s=await api('/api/v1/dashboard/session');if(s.mode==='disabled'||(s.mode==='protected'&&!s.authenticated)){location.href='/';return}await load('day')}catch{location.href='/'}
}
document.querySelectorAll('.range-btn').forEach(b=>b.addEventListener('click',()=>load(b.dataset.range)));
let resizeTimer;window.addEventListener('resize',()=>{clearTimeout(resizeTimer);resizeTimer=setTimeout(()=>{if(currentData)renderSeries(currentData)},120)});
boot();
