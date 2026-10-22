const UI_VERSION='0.4.28-alpha';
const UI_BASE=`/ui/${UI_VERSION}`;
const $=s=>document.querySelector(s);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const dateLocal=d=>[d.getFullYear(),String(d.getMonth()+1).padStart(2,'0'),String(d.getDate()).padStart(2,'0')].join('-');
const fmtTraffic=v=>{
  let n=Number(v);
  if(!Number.isFinite(n)||n<0)return '—';
  const units=['B','KB','MB','GB','TB','PB'];let i=0;
  while(n>=1000&&i<units.length-1){n/=1000;i++}
  return `${n.toFixed(i?2:0)} ${units[i]}`;
};
const directionText={inbound:'入站',outbound:'出站',total:'双向合计'};
// Live cycle usage is a different metric from the historical ranking window.
function rankingUsageInfo(node){
  const quota=Number(node.quota_bytes);
  if(!(quota>0))return {tier:'none',label:'未设置额度',percent:null};
  if(!node.cycle_start||String(node.cycle_start).startsWith('0001-'))return {tier:'none',label:'暂无计费数据',percent:null};
  const percent=Number(node.usage_percent);
  if(!Number.isFinite(percent)||percent<0)return {tier:'none',label:'暂无计费数据',percent:null};
  const tier=percent>90?'danger':percent>=60?'warning':'good';
  return {tier,label:`${percent.toFixed(1)}%`,percent};
}
function renderRankingUsage(node){
  const x=rankingUsageInfo(node);
  if(x.percent===null)return `<span class="traffic-usage-empty">${esc(x.label)}</span>`;
  const value=Math.min(100,x.percent);
  return `<div class="traffic-usage traffic-usage-${x.tier}">
    <strong class="traffic-usage-label">${esc(x.label)}</strong>
    <div class="traffic-usage-track" role="progressbar" aria-label="${esc('流量使用率 '+x.label)}" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${value.toFixed(1)}" aria-valuetext="${esc(x.label)}"><span style="width:${value.toFixed(1)}%"></span></div>
  </div>`;
}
// Calendar-day distance prevents off-by-one errors from UTC parsing and DST.
function rankingExpiryInfo(raw,today=new Date()){
  const value=String(raw??'').trim();
  if(!value)return {date:'—',note:'未设置到期日',state:'missing'};
  if(value==='2036-01-01'||['l','long','longterm','long-term','长期'].includes(value.toLowerCase()))return {date:'长期',note:'无需倒计时',state:'longterm'};
  if(!/^\d{4}-\d{2}-\d{2}$/.test(value))return {date:'—',note:'到期日无效',state:'missing'};
  const [year,month,day]=value.split('-').map(Number);
  const target=Date.UTC(year,month-1,day);
  const verify=new Date(target);
  if(verify.getUTCFullYear()!==year||verify.getUTCMonth()!==month-1||verify.getUTCDate()!==day)return {date:'—',note:'到期日无效',state:'missing'};
  const todayUTC=Date.UTC(today.getFullYear(),today.getMonth(),today.getDate());
  const days=Math.round((target-todayUTC)/86400000);
  if(days<0)return {date:value,note:`已到期 ${-days} 天`,state:'expired'};
  if(days===0)return {date:value,note:'今天到期',state:'urgent'};
  return {date:value,note:`剩余 ${days} 天`,state:days<=7?'urgent':'normal'};
}
function renderRankingExpiry(raw){
  const x=rankingExpiryInfo(raw);
  return `<span class="traffic-expiry traffic-expiry-${x.state}"><strong>${esc(x.date)}</strong><span>${esc(x.note)}</span></span>`;
}

let current=null,selectedDay='';
let viewerTz=-new Date().getTimezoneOffset();

async function api(url){
  const r=await fetch(url,{credentials:'same-origin',cache:'no-store'});
  if(!r.ok){const err=new Error((await r.text()).trim()||`HTTP ${r.status}`);err.status=r.status;throw err}
  return r.json();
}
function showError(message){
  const el=$('#trafficError');el.textContent=message;el.classList.remove('hidden');
}
function hideError(){$('#trafficError').classList.add('hidden')}
function updateQuick(days){
  document.querySelectorAll('[data-days]').forEach(el=>el.classList.toggle('active',Number(el.dataset.days)===days));
}
function setRange(days){
  const end=new Date(),start=new Date(end.getFullYear(),end.getMonth(),end.getDate());
  start.setDate(start.getDate()-days+1);
  $('#dateFrom').value=dateLocal(start);
  $('#dateTo').value=dateLocal(end);
  updateQuick(days);
}
function selectedNode(){return current?.nodes?.find(x=>x.id===$('#trafficNodeSelect').value)||null}
function selectedSeries(){const node=selectedNode();return node?.days||current?.days||[]}
function metricCard(name,value,note){
  return `<article class="traffic-kpi"><div class="traffic-kpi-name">${esc(name)}</div><div class="traffic-kpi-value">${esc(value)}</div><div class="small">${esc(note)}</div></article>`;
}
function renderCards(){
  if(!current)return;
  const node=selectedNode(),total=node?.history||current.totals||{};
  const quota=node?.quota_bytes||0,used=node?.cycle_used_bytes||0;
  const notes=node?'该服务器的已归档历史':'所有服务器的已归档历史';
  const items=[
    metricCard('区间入站',fmtTraffic(total.inbound_bytes),notes),
    metricCard('区间出站',fmtTraffic(total.outbound_bytes),notes),
    metricCard('区间计费流量',fmtTraffic(total.used_bytes),'同一周期的已确认增量'),
  ];
  if(node){
    items.push(metricCard('本周期已用',fmtTraffic(used),node.cycle_start?'Agent 计费周期累计':'暂无 Agent 周期数据'));
    items.push(metricCard('本周期额度',quota?fmtTraffic(quota):'未设置',directionText[node.direction]||'按 Agent 配置'));
    items.push(metricCard('剩余额度',quota?fmtTraffic(node.remaining_bytes):'—',quota?`占用 ${Math.max(0,Number(node.usage_percent||0)).toFixed(1)}%`:'未配置流量限额'));
  } else {
    const withQuota=current.nodes.filter(x=>x.quota_bytes>0);
    const sum=(field)=>withQuota.reduce((acc,n)=>acc+Number(n[field]||0),0);
    items.push(metricCard('本周期已用',withQuota.length?fmtTraffic(sum('cycle_used_bytes')):'—',`${withQuota.length} 台有额度的服务器`));
    items.push(metricCard('总计费额度',withQuota.length?fmtTraffic(sum('quota_bytes')):'—','各 Agent 当前周期配置总和'));
    items.push(metricCard('合计剩余额度',withQuota.length?fmtTraffic(sum('remaining_bytes')):'—','跨计费周期合计仅供参考'));
  }
  $('#trafficCards').innerHTML=items.join('');
}
function renderChart(){
  const days=selectedSeries(),max=Math.max(1,...days.map(d=>Math.max(Number(d.inbound_bytes)||0,Number(d.outbound_bytes)||0)));
  const scroll=$('#trafficChart');
  if(!days.length){scroll.innerHTML='<div class="traffic-empty">尚无可显示日期</div>';return}
  scroll.innerHTML=`<div class="traffic-bar-grid">${days.map((d,i)=>{
    const inb=Number(d.inbound_bytes)||0,outb=Number(d.outbound_bytes)||0;
    const hi=inb?Math.max(3,100*inb/max):0,ho=outb?Math.max(3,100*outb/max):0;
    const label=esc(d.date),note=`${d.date}：入站 ${fmtTraffic(inb)}，出站 ${fmtTraffic(outb)}，计费 ${fmtTraffic(d.used_bytes)}${d.incomplete?'（数据缺口）':''}`;
    return `<button type="button" class="traffic-day ${d.date===selectedDay?'selected':''}" data-day="${label}" title="${esc(note)}" aria-label="${esc(note)}">
      <div class="traffic-bar-area"><span class="traffic-bar-in" style="height:${hi.toFixed(2)}%"></span><span class="traffic-bar-out" style="height:${ho.toFixed(2)}%"></span></div>
      <div class="traffic-day-label">${i%Math.max(1,Math.ceil(days.length/12))===0||i===days.length-1?esc(d.date.slice(5)):'·'}</div>
      ${d.incomplete?'<span class="traffic-gap-dot" title="数据不完整">!</span>':''}
    </button>`;
  }).join('')}</div>`;
  const last=days.find(x=>x.date===selectedDay)||days[days.length-1];selectedDay=last.date;
  showDayDetails(last);
}
function showDayDetails(day){
  $('#trafficDayDetails').textContent=`${day.date} · 入站 ${fmtTraffic(day.inbound_bytes)} · 出站 ${fmtTraffic(day.outbound_bytes)} · 计费 ${fmtTraffic(day.used_bytes)}${day.incomplete?' · 数据不完整，请勿作为精确账单':''}`;
}
function renderRanking(){
  if(!current)return;
  const rows=current.nodes||[];
  const selected=$('#trafficNodeSelect').value;
  const table=`<table class="traffic-table">
    <thead><tr><th>排名 / 服务器</th><th>区间计费</th><th>区间入站</th><th>区间出站</th><th>本周期已用 / 额度</th><th>剩余额度</th><th>流量使用情况</th><th>订阅到期</th></tr></thead>
    <tbody>${rows.map((node,index)=>{
      const hist=node.history||{},quota=node.quota_bytes||0;
      return `<tr class="${selected===node.id?'traffic-selected-row':''}">
        <td><button class="traffic-node-link" type="button" data-node="${esc(node.id)}"><strong>${index+1}. ${esc(node.display_name||node.id)}</strong></button><span class="traffic-state ${node.online?'online':'offline'}">${node.online?'在线':'离线 / 未更新'}</span>${node.history_error?'<span class="traffic-gap-text">历史读取失败 · 实时计费仍可查看</span>':node.incomplete?'<span class="traffic-gap-text">部分数据缺口</span>':''}</td>
        <td>${fmtTraffic(hist.used_bytes)}</td><td>${fmtTraffic(hist.inbound_bytes)}</td><td>${fmtTraffic(hist.outbound_bytes)}</td>
        <td>${quota?`${fmtTraffic(node.cycle_used_bytes)} / ${fmtTraffic(quota)}`:'未设置额度'}</td>
        <td>${quota?fmtTraffic(node.remaining_bytes):'—'}</td>
        <td>${renderRankingUsage(node)}</td><td>${renderRankingExpiry(node.expire_at)}</td>
      </tr>`;
    }).join('')}</tbody></table>`;
  $('#trafficRanking').innerHTML=rows.length?table:'<div class="traffic-empty">尚未添加服务器</div>';
}
function render(){
  const currentSel=$('#trafficNodeSelect').value;
  $('#trafficNodeSelect').innerHTML='<option value="">全部服务器</option>'+current.nodes.map(n=>`<option value="${esc(n.id)}">${esc(n.display_name||n.id)}</option>`).join('');
  if(current.nodes.some(n=>n.id===currentSel))$('#trafficNodeSelect').value=currentSel;
  $('#trafficRangeLabel').textContent=`${current.from} 至 ${current.to} · 时区 UTC${viewerTz>=0?'+':''}${(viewerTz/60).toFixed(2)} · 每 ${current.bucket_minutes} 分钟归档`;
  $('#trafficStatus').textContent=`${current.nodes.length} 台服务器 · ${current.retention_days} 天历史`;
  $('#trafficFootnote').textContent=`每台服务器第一个采样点只作为基准，不能追溯安装前流量。超过 2 小时的数据缺口不强行计算；当前计费数据来自最后一次 Agent 上报，离线时可能已经过期。历史最长 ${current.retention_days} 天。`;
  renderCards();renderChart();renderRanking();
  const failed=(current.nodes||[]).filter(n=>n.history_error).length;
  if(failed)showError(`${failed} 台服务器的历史暂不可读取，其他节点统计及现有计费状态不受影响。请通过 SSH 检查 Server 存储与权限。`);
}
async function load(){
  hideError();
  const from=$('#dateFrom').value,to=$('#dateTo').value;
  if(!from||!to||from>to){showError('请选择正确的开始和结束日期');return}
  try{
    const url=`/api/v1/traffic/dashboard?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&tz=${viewerTz}`;
    current=await api(url);selectedDay='';
    render();
    $('#trafficLoading').classList.add('hidden');
    $('#trafficView').classList.remove('hidden');
  }catch(err){
    if(err.status===401||err.status===404){location.href=`${UI_BASE}/`;return}
    $('#trafficLoading').classList.add('hidden');
    $('#trafficView').classList.remove('hidden');
    showError(err.message||'获取统计信息失败');
  }
}
async function boot(){
  setRange(30);
  try{
    const s=await api('/api/v1/dashboard/session');
    const liveVersion=String(s.server_version||'').trim();
    if(liveVersion&&liveVersion!==UI_VERSION){location.replace(`/ui/${encodeURIComponent(liveVersion)}/traffic.html`);return}
    if(s.mode==='disabled'||(s.mode==='protected'&&!s.authenticated)){location.href=`${UI_BASE}/`;return}
    await load();
  }catch{location.href=`${UI_BASE}/`}
}
document.querySelectorAll('[data-days]').forEach(b=>b.addEventListener('click',()=>{setRange(Number(b.dataset.days));load()}));
$('#trafficDates').addEventListener('submit',e=>{e.preventDefault();updateQuick(0);load()});
$('#trafficNodeSelect').addEventListener('change',()=>{selectedDay='';renderCards();renderChart();renderRanking()});
$('#trafficChart').addEventListener('click',e=>{
  const button=e.target.closest('.traffic-day');if(!button)return;
  selectedDay=button.dataset.day;
  document.querySelectorAll('.traffic-day').forEach(el=>el.classList.toggle('selected',el===button));
  const day=selectedSeries().find(x=>x.date===selectedDay);if(day)showDayDetails(day);
});
$('#trafficRanking').addEventListener('click',e=>{
  const button=e.target.closest('[data-node]');if(!button)return;
  $('#trafficNodeSelect').value=button.dataset.node;
  selectedDay='';renderCards();renderChart();renderRanking();
  $('#trafficChart').scrollIntoView({behavior:'smooth',block:'nearest'});
});
boot();
