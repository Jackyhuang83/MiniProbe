"use strict";
(() => {
  const api = "/api/v1/subscriptions";
  const $ = id => document.getElementById(id);
  let items = [];
  let renewals = [];
  let reminderConfig = {enabled:false,days:[]};
  const categoryText = {vps:"VPS",video:"影视 / IPTV",ai:"AI 服务",domain:"域名 / 云服务",software:"软件 / App",other:"其他订阅"};
  const categoryIcon = {vps:"▣",video:"▶",ai:"✧",domain:"◎",software:"▦",other:"◇"};
  const statusText = {active:"有效",paused:"已暂停",cancelled:"已取消",expired:"已到期"};
  const cycleText = {monthly:"月度",quarterly:"季度",yearly:"年度",custom:"自定义",long_term:"长期"};
  // Node settings currently provide a monthly price, not the actual billing interval or renewal mode.
  // Never claim that a VPS is monthly-billed or manually renewed when the source did not say so.
  function billingBadge(item) {
    if (item.source==="vps" && item.cycle!=="long_term") return {label:"月租",style:"node"};
    const label=cycleText[item.cycle]||"周期未设置";
    return {label:item.cycle==="custom"&&item.custom_days?label+" · "+item.custom_days+"天":label,style:item.cycle||"unknown"};
  }
  function renewalLabel(item) {
    if(item.source==="vps") return {label:"续费方式未设置",style:"unknown",icon:"○"};
    return item.auto_renew===true?{label:"自动续费",style:"automatic",icon:"↻"}:{label:"手动续费",style:"manual",icon:"◷"};
  }
  function alertText(message) {$("subNotice").textContent=message;$("subNotice").classList.remove("hidden");}
  function clearAlert() {$("subNotice").classList.add("hidden");$("subNotice").textContent="";}
  async function request(path, options={}) {
    const response=await fetch(api+path,{credentials:"same-origin",cache:"no-store",...options,headers:{"X-MiniProbe-Request":"1",...(options.headers||{})}});
    if (!response.ok) throw new Error(response.status===401?"访问未授权，请重新登录":response.status===403?"必须通过 HTTPS 安全访问":response.status===429?"尝试次数过多，请稍后再试":"服务器请求失败 ("+response.status+")");
    return response.json();
  }
  function authView(yes) {
    $("subGate").classList.toggle("hidden",yes);
    $("subPrivate").classList.toggle("hidden",!yes);
    $("subLogout").classList.toggle("hidden",!yes);
  }
  function decimal(currency) {return ["JPY","KRW","VND","CLP","ISK","PYG","RWF","UGX","XAF","XOF"].includes(currency)?0:["BHD","KWD","OMR","JOD","TND"].includes(currency)?3:2;}
  function price(item) {
    if (!item.currency) return "未设置";
    const digits=decimal(item.currency);
    const amount=Number(item.amount_minor)/Math.pow(10,digits);
    if (!Number.isFinite(amount)) return "—";
    return amount.toLocaleString("zh-CN",{minimumFractionDigits:digits,maximumFractionDigits:digits});
  }
  function todayAt(timezone) {
    const now=new Date();
    try {
      const parts=Object.fromEntries(new Intl.DateTimeFormat("en-US",{timeZone:timezone||"Asia/Tokyo",year:"numeric",month:"2-digit",day:"2-digit"}).formatToParts(now).filter(x=>x.type!=="literal").map(x=>[x.type,x.value]));
      return [parts.year,parts.month,parts.day].join("-");
    } catch (_) {return now.toISOString().slice(0,10);}
  }
  function daysUntil(ymd,zone) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(ymd||"")) return null;
    const utc=x=>Date.UTC(Number(x.slice(0,4)),Number(x.slice(5,7))-1,Number(x.slice(8,10)));
    return Math.round((utc(ymd)-utc(todayAt(zone)))/86400000);
  }
  function effectiveStatus(item) {
    if (item.status==="active" && daysUntil(item.due_date,item.time_zone)<0) return "expired";
    return item.status||"active";
  }
  function el(tag,className,text) {
    const n=document.createElement(tag);
    if(className)n.className=className;
    if(text!==undefined)n.textContent=String(text);
    return n;
  }
  function card(item) {
    const node=el("article","sub-card");
    const head=el("div","sub-card-head");
    head.append(el("div","sub-avatar "+item.category,categoryIcon[item.category]||"◇"));
    const heading=el("div","sub-card-title");
    heading.append(el("strong","",item.name||"未命名"),el("small","",categoryText[item.category]||"其他订阅"));
    head.append(heading);
    node.append(head);
    const row=el("div","sub-card-state");
    const state=effectiveStatus(item);
    row.append(el("span","sub-tag "+(state==="active"?"good":state==="expired"?"bad":"warn"),statusText[state]||state));
    if(item.source==="vps")row.append(el("span","sub-tag","VPS 自动同步"));
    // Renewal mode belongs near the due date; keep the header focused on status.
    node.append(row,el("div","sub-divider"));
    const total=el("div","sub-card-price",price(item));
    total.append(el("small","",item.currency?" "+item.currency:""));
    const priceRow=el("div","sub-price-row");
    const billingTag=billingBadge(item);
    priceRow.append(total,el("span","sub-cycle-badge "+billingTag.style,billingTag.label));
    node.append(priceRow);
    if(item.source==="vps") node.append(el("div","sub-cycle-note","节点月租金额 · 实际扣费周期未设置"));
    const days=daysUntil(item.due_date,item.time_zone);
    let badge="未设置";
    if(item.cycle==="long_term")badge="长期";
    else if(days!==null){badge=days<0?"已逾期 "+Math.abs(days)+" 天":days===0?"今天到期":days+" 天后";}
    const due=el("div","sub-due");
    due.append(el("span","",item.cycle==="long_term"?"服务期限":"下次到期"));
    due.append(el("b",days!==null&&days<=7?"danger":"",badge));
    node.append(due);
    const renewal=renewalLabel(item);
    node.append(el("div","sub-renew "+renewal.style,renewal.icon+" "+renewal.label));
    let meta=item.due_date||"无到期日期";
    if(item.source==="vps")meta+=" · 来源："+(item.node_id||"节点");
    if(item.tags?.length)meta+=" · "+item.tags.join(", ");
    node.append(el("div","sub-meta",meta));
    return node;
  }
  function renderRenewals() {
    $("subReminderState").textContent = reminderConfig.enabled ? "到期提醒：已开启 · "+reminderConfig.days.map(x=>x===-1?"逾期一次":x===0?"当天":x+" 天前").join("、") : "到期提醒：未开启（SSH 菜单 15 → 8 设置）";
    const names=new Map(items.map(x=>[x.id,x.name]));
    const list=$("subRenewalHistory");
    list.replaceChildren(...renewals.slice(0,12).map(record=>{
      const line=el("div","sub-history-row");
      const desc=el("div","sub-history-desc");
      desc.append(el("strong","",names.get(record.subscription_id)||"已删除的订阅"),el("small","",record.date+(record.new_due_date?" · 新到期 "+record.new_due_date:"")));
      const amount=el("b","",price(record));
      amount.append(el("small","",record.currency?" "+record.currency:""));
      line.append(desc,amount);
      return line;
    }));
    $("subRenewalEmpty").classList.toggle("hidden",renewals.length!==0);
  }

  // DEV3: forecasts are projections, not actual charges. Never mix currencies.
  function nextCycleDate(date,cycle,customDays) {
    if(!/^\d{4}-\d{2}-\d{2}$/.test(date||""))return null;
    const [year,month,day]=date.split("-").map(Number);
    const base=new Date(Date.UTC(year,month-1,day));
    if(Number.isNaN(base.getTime())||base.toISOString().slice(0,10)!==date)return null;
    if(cycle==="custom"&&customDays>0)base.setUTCDate(base.getUTCDate()+customDays);
    else {
      const months={monthly:1,quarterly:3,yearly:12}[cycle];
      if(!months)return null;
      const target=new Date(Date.UTC(year,month-1+months,1));
      const end=new Date(Date.UTC(target.getUTCFullYear(),target.getUTCMonth()+1,0)).getUTCDate();
      target.setUTCDate(Math.min(day,end));
      return target.toISOString().slice(0,10);
    }
    return base.toISOString().slice(0,10);
  }
  function projectedDates(item,year) {
    if(item.source==="vps"||item.status!=="active"||!item.due_date||item.cycle==="long_term")return [];
    let current=item.due_date,dates=[],seen=new Set();
    // Forecast only from known due dates; don't invent historical payments.
    for(let n=0;n<500;n++){
      if(seen.has(current))break;seen.add(current);
      if(current.slice(0,4)>String(year))break;
      if(current.slice(0,4)===String(year))dates.push(current);
      const next=nextCycleDate(current,item.cycle,item.custom_days);
      if(!next||next<=current)break;current=next;
    }
    return dates;
  }
  function renderInsights(){
    const year=Number($("subYear").value)||new Date().getFullYear();
    const totals=new Map(),monthTotals=Array.from({length:12},()=>new Map()),calendar=new Map();
    let excluded=0;
    for(const item of items){
      const dates=projectedDates(item,year);
      if(item.source==="vps"&&item.amount_minor>0)excluded++;
      for(const date of dates){
        const currency=item.currency;
        if(!currency)continue;
        const amount=Number(item.amount_minor);
        if(!Number.isSafeInteger(amount)||amount<0)continue;
        totals.set(currency,(totals.get(currency)||0)+amount);
        const month=Number(date.slice(5,7))-1;
        monthTotals[month].set(currency,(monthTotals[month].get(currency)||0)+amount);
        if(!calendar.has(date))calendar.set(date,[]);
        calendar.get(date).push(item.name);
      }
    }
    const fmt=map=>[...map].sort((a,b)=>a[0].localeCompare(b[0])).map(([currency,amount])=>price({currency,amount_minor:amount})+" "+currency).join(" · ")||"—";
    $("subForecastTotal").textContent=fmt(totals);
    $("subForecastCaveat").textContent="仅预测已设置周期、有效且有到期日的手动订阅；"+excluded+" 个有月租的 VPS 未纳入，避免假设扣费周期。";
    $("subMonthRows").replaceChildren(...monthTotals.map((total,i)=>{
      const line=el("div","sub-month-row");
      line.append(el("span","",String(i+1).padStart(2,"0")+" 月"),el("strong","",fmt(total)));
      return line;
    }));
    const month=Number($("subCalendarMonth").value);
    const first=new Date(Date.UTC(year,month-1,1)).getUTCDay();
    const count=new Date(Date.UTC(year,month,0)).getUTCDate();
    const cells=[];
    for(let i=0;i<first;i++)cells.push(el("div","sub-calendar-blank",""));
    for(let d=1;d<=count;d++){
      const date=year+"-"+String(month).padStart(2,"0")+"-"+String(d).padStart(2,"0");
      const names=calendar.get(date)||[];
      const cell=el("div","sub-calendar-day"+(names.length?" has-due":""));
      cell.append(el("b","",d));
      if(names.length)cell.append(el("small","",names.length+" 笔预计到期"),el("span","",names.slice(0,2).join("、")));
      cells.push(cell);
    }
    $("subCalendarGrid").replaceChildren(...cells);
    $("subCalendarSummary").textContent="本月 "+[...calendar.keys()].filter(d=>d.startsWith(year+"-"+String(month).padStart(2,"0"))).reduce((n,d)=>n+calendar.get(d).length,0)+" 笔预计到期 · VPS 日期不参与周期推算";
  }

  function render() {
    const q=$("subQuery").value.trim().toLocaleLowerCase();
    const cat=$("subCategory").value;
    const status=$("subStatus").value;
    const selected=items.filter(x=>(!cat||x.category===cat)&&(!status||effectiveStatus(x)===status)&&(!q||(x.name+" "+(x.tags||[]).join(" ")).toLocaleLowerCase().includes(q)));
    const cards=$("subCards");
    cards.replaceChildren(...selected.map(card));
    $("subShown").textContent="显示 "+selected.length+" / "+items.length;
    $("subEmpty").classList.toggle("hidden",selected.length!==0);
    $("subTotal").textContent=String(items.length);
    $("subVPS").textContent=String(items.filter(x=>x.source==="vps").length);
    $("subDue30").textContent=String(items.filter(x=>{const d=daysUntil(x.due_date,x.time_zone);return d!==null&&d>=0&&d<=30&&effectiveStatus(x)==="active";}).length);
    $("subUpdated").textContent="更新于 "+new Date().toLocaleTimeString("zh-CN",{hour:"2-digit",minute:"2-digit"});
  }
  async function load() {
    const result=await request("");
    items=Array.isArray(result.items)?result.items:[];
    renewals=Array.isArray(result.renewals)?result.renewals:[];
    reminderConfig=result.reminders||{enabled:false,days:[]};
    clearAlert();authView(true);render();renderRenewals();renderInsights();
  }
  async function init() {
    try {
      const s=await request("/session");
      if(!s.secure){
        authView(false);
        $("subGateText").textContent="当前连接不是安全的 HTTPS。为了保护私人费用信息，订阅中心拒绝通过 HTTP 登录或读取。请使用已配置的 HTTPS Tunnel。";
        $("subLogin").classList.add("hidden");
        return;
      }
      if(!s.enabled){
        $("subGateText").textContent="尚未设置独立订阅密码。请先在 Server SSH 执行 miniprobe → 15 → 6，然后通过 HTTPS 登录。";
        $("subLogin").classList.add("hidden");
        authView(false);return;
      }
      if(s.authenticated)await load();
      else authView(false);
    }catch(e){authView(false);alertText(e.message);}
  }
  $("subLogin").addEventListener("submit",async event=>{
    event.preventDefault();clearAlert();
    const password=$("subPassword").value;
    try {
      await request("/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({password})});
      $("subPassword").value="";
      await load();
    }catch(e){alertText(e.message);}
  });
  $("subLogout").addEventListener("click",async()=>{
    try{await request("/logout",{method:"POST"});}
    catch(e){alertText(e.message);return;}
    items=[];authView(false);
  });
  for(const id of ["subQuery","subCategory","subStatus"])$(id).addEventListener(id==="subQuery"?"input":"change",render);
  const currentYear=new Date().getFullYear();
  for(let y=currentYear-1;y<=currentYear+2;y++){$("subYear").append(el("option","",y));$("subYear").lastChild.value=String(y);}
  $("subYear").value=String(currentYear);
  $("subCalendarMonth").value=String(new Date().getMonth()+1);
  for(const id of ["subYear","subCalendarMonth"])$(id).addEventListener("change",renderInsights);
  init();
})();