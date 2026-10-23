"use strict";
(() => {
  const api = "/api/v1/subscriptions";
  const $ = id => document.getElementById(id);
  let items = [];
  const categoryText = {vps:"VPS",video:"影视 / IPTV",ai:"AI 服务",domain:"域名 / 云服务",software:"软件 / App",other:"其他订阅"};
  const categoryIcon = {vps:"▣",video:"▶",ai:"✧",domain:"◎",software:"▦",other:"◇"};
  const statusText = {active:"有效",paused:"已暂停",cancelled:"已取消",expired:"已到期"};
  const cycleText = {monthly:"月付",quarterly:"季付",yearly:"年付",custom:"自定义",long_term:"长期"};
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
    if(item.auto_renew)row.append(el("span","sub-tag","自动续费"));
    node.append(row,el("div","sub-divider"));
    const total=el("div","sub-card-price",price(item));
    total.append(el("small","",item.currency?" "+item.currency:""));
    node.append(total);
    node.append(el("div","sub-cycle",cycleText[item.cycle]||"自定义"+(item.custom_days?" · 每 "+item.custom_days+" 天":"") ));
    const days=daysUntil(item.due_date,item.time_zone);
    let badge="未设置";
    if(item.cycle==="long_term")badge="长期";
    else if(days!==null){badge=days<0?"已逾期 "+Math.abs(days)+" 天":days===0?"今天到期":days+" 天后";}
    const due=el("div","sub-due");
    due.append(el("span","",item.cycle==="long_term"?"服务期限":"下次到期"));
    due.append(el("b",days!==null&&days<=7?"danger":"",badge));
    node.append(due);
    let meta=item.due_date||"无到期日期";
    if(item.source==="vps")meta+=" · 来源："+(item.node_id||"节点");
    if(item.tags?.length)meta+=" · "+item.tags.join(", ");
    node.append(el("div","sub-meta",meta));
    return node;
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
    clearAlert();authView(true);render();
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
  init();
})();