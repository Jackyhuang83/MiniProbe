"use strict";
(() => {
  const api = "/api/v1/subscriptions";
  const $ = id => document.getElementById(id);
  let items = [];
  let renewals = [];
  let reminderConfig = {enabled:false,days:[]};
  let csrf = "";
  let accessMode = false;
  let editingID = "";
  let renewingID = "";
  let busy = false;
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
  function alertText(message,success=false) {$("subNotice").textContent=message;$("subNotice").classList.remove("hidden");$("subNotice").classList.toggle("success",success);}
  function clearAlert() {$("subNotice").classList.add("hidden");$("subNotice").classList.remove("success");$("subNotice").textContent="";}
  async function request(path, options={}) {
    const write=path==="/manage"||path.startsWith("/manage?")||path==="/renewals"||path==="/email/reset-password";
    if(write&&!csrf)throw new Error("安全会话未就绪，请重新登录。");
    const headers={"X-MiniProbe-Request":"1","X-Requested-With":"XMLHttpRequest",...(write?{"Content-Type":"application/json","X-MiniProbe-Subscription-CSRF":csrf}:{}),...(options.headers||{})};
    const response=await fetch(api+path,{credentials:"same-origin",cache:"no-store",...options,headers});
    if (!response.ok) {
      const details=(response.status===400||response.status===409)&&typeof response.text==="function"?(await response.text()).trim().slice(0,160):"";
      throw new Error(details||({401:"访问未授权，请重新登录",403:"安全校验失败：请确认 HTTPS、登录状态及页面未过期",404:"记录不存在，请刷新页面",429:"请求过于频繁，请稍后再试",503:"SMTP 发送失败，请使用本地管理菜单检查邮件设置"}[response.status]||"服务器请求失败 ("+response.status+")"));
    }
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
    const actions=el("div","sub-card-actions");
    if(item.source==="manual"){
      const edit=el("button","","编辑");
      edit.type="button";edit.addEventListener("click",()=>openEditor(item));
      actions.append(edit);
    }
    const renew=el("button","",item.source==="vps"?"记录付款":"记录续费");
    renew.type="button";renew.addEventListener("click",()=>openRenewal(item));
    actions.append(renew);
    if(item.source==="manual"){
      const del=el("button","sub-danger","删除");
      del.type="button";del.addEventListener("click",()=>deleteSubscription(item));
      actions.append(del);
    }
    node.append(actions);
    return node;
  }
  function renderRenewals() {
    $("subReminderState").textContent = reminderConfig.enabled ? "到期提醒：已开启 · "+reminderConfig.days.map(x=>x===-1?"逾期一次":x===0?"当天":x+" 天前").join("、") : "到期提醒：未开启（本地菜单 15 → 8 设置）";
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
  function minorFromInput(value,currency){
    const raw=String(value).trim();
    if(!/^\d{1,10}(\.\d{1,3})?$/.test(raw))throw new Error("金额只能输入非负数字，最多三位小数。");
    const [whole,part=""]=raw.split(".");
    const digits=decimal(currency);
    if(part.length>digits)throw new Error(currency+" 最多支持 "+digits+" 位小数。");
    const minor=Number(whole)*Math.pow(10,digits)+Number((part+"0".repeat(digits)).slice(0,digits)||0);
    if(!Number.isSafeInteger(minor)||minor>1e12)throw new Error("金额超出允许范围。");
    return minor;
  }
  function amountRaw(item){
    const digits=decimal(item.currency||"USD");
    return (Number(item.amount_minor||0)/Math.pow(10,digits)).toFixed(digits);
  }
  function closePanels(){
    $("subEditor").classList.add("hidden");
    $("subRenewEditor").classList.add("hidden");
    editingID="";renewingID="";
  }
  function reveal(id){
    $(id).classList.remove("hidden");
    $(id).scrollIntoView?.({block:"start",behavior:"smooth"});
  }
  function refreshCycle(){
    const cycle=$("subEditCycle").value;
    $("subEditCustomWrap").classList.toggle("hidden",cycle!=="custom");
    $("subEditCustomDays").required=cycle==="custom";
    $("subEditDueWrap").classList.toggle("hidden",cycle==="long_term");
    $("subEditDue").required=cycle!=="long_term";
  }
  function openEditor(item=null){
    if(busy)return;
    closePanels();clearAlert();
    editingID=item?.source==="manual"?item.id:"";
    $("subEditorTitle").textContent=editingID?"编辑订阅":"添加订阅";
    $("subEditName").value=item?.name||"";
    $("subEditCategory").value=item?.category||"video";
    $("subEditCurrency").value=item?.currency||"USD";
    $("subEditAmount").value=item?amountRaw(item):"";
    $("subEditCycle").value=item?.cycle||"monthly";
    $("subEditCustomDays").value=item?.custom_days||"";
    $("subEditDue").value=item?.due_date||"";
    $("subEditStatus").value=item?.status||"active";
    $("subEditZone").value=item?.time_zone||"Asia/Tokyo";
    $("subEditAuto").checked=!!item?.auto_renew;
    $("subEditTags").value=(item?.tags||[]).join(", ");
    $("subEditNotes").value=item?.notes||"";
    refreshCycle();
    reveal("subEditor");
    $("subEditName").focus?.();
  }
  function openRenewal(item){
    if(busy)return;
    closePanels();clearAlert();
    renewingID=item.id;
    $("subRenewTitle").textContent="记录续费 · "+item.name;
    const canChangeDue=item.source==="manual"&&item.cycle!=="long_term";
    $("subRenewDueWrap").classList.toggle("hidden",!canChangeDue);
    $("subRenewDue").value="";
    $("subRenewHint").textContent=canChangeDue?"可填写续费后的新到期日；不填写则只追加付款记录。":"VPS 到期日仍由节点菜单管理；这里只记录实际付款。";
    $("subRenewDate").value=todayAt(item.time_zone);
    $("subRenewCurrency").value=item.currency||"USD";
    $("subRenewAmount").value=amountRaw(item);
    $("subRenewNotes").value="";
    reveal("subRenewEditor");
  }
  async function mutate(path,method,payload,success,button){
    if(busy)return;
    busy=true;button.disabled=true;
    try{
      await request(path,{method,body:payload===undefined?undefined:JSON.stringify(payload)});
      await load();
      closePanels();
      alertText(success,true);
    }catch(e){alertText(e.message);}
    finally{busy=false;button.disabled=false;}
  }
  function subscriptionPayload(){
    const currency=$("subEditCurrency").value.trim().toUpperCase();
    const cycle=$("subEditCycle").value;
    const rec={
      name:$("subEditName").value.trim(),
      category:$("subEditCategory").value,
      amount_minor:minorFromInput($("subEditAmount").value,currency),
      currency,
      cycle,
      custom_days:cycle==="custom"?Number($("subEditCustomDays").value):0,
      due_date:cycle==="long_term"?"":$("subEditDue").value,
      time_zone:$("subEditZone").value.trim(),
      auto_renew:$("subEditAuto").checked,
      status:$("subEditStatus").value,
      tags:$("subEditTags").value.split(",").map(x=>x.trim()).filter(Boolean),
      notes:$("subEditNotes").value.trim()
    };
    if(editingID)rec.id=editingID;
    return rec;
  }
  async function deleteSubscription(item){
    if(busy||item.source!=="manual")return;
    if(!window.confirm("永久删除「"+item.name+"」？\\n历史付款记录将保留，但订阅卡片会删除。"))return;
    await mutate("/manage?id="+encodeURIComponent(item.id),"DELETE",undefined,"已删除订阅："+item.name,document.createElement("button"));
  }
  async function init() {
    try {
      const s=await request("/session");
      csrf=s.authenticated?(s.csrf||""):"";
      accessMode=s.access_mode===true;
      if(accessMode){
        $("subLogin").classList.add("hidden");
        $("subEmailLogin").classList.add("hidden");
        $("subEmailToggle").classList.add("hidden");
        if(!s.authenticated){
          $("subGateText").textContent="Cloudflare Access 尚未验证登录或 Access 令牌已过期。请重新打开订阅专用域名并输入邮箱验证码。";
          authView(false);return;
        }
        await load();
        return;
      }
      $("subEmailToggle").classList.toggle("hidden",!s.email_available||s.authenticated);
      $("subEmailResetSection").classList.toggle("hidden",!s.authenticated||!s.email_reset_allowed);
      if(!s.secure){
        authView(false);
        $("subGateText").textContent="当前连接不是安全的 HTTPS。为了保护私人费用信息，订阅中心拒绝通过 HTTP 登录或读取。请使用已配置的 HTTPS Tunnel。";
        $("subLogin").classList.add("hidden");
        return;
      }
      if(!s.enabled){
        $("subGateText").textContent="尚未设置独立订阅密码。Docker 版请进入容器内 MiniProbe 管理菜单 → 15 → 6 设置（可通过 Docker 管理脚本打开）；原生版请在 SSH 执行 miniprobe → 15 → 6。设置后通过 HTTPS 登录。";
        $("subLogin").classList.add("hidden");
        $("subEmailToggle").classList.add("hidden");
        authView(false);return;
      }
      if(s.authenticated)await load();
      else authView(false);
    }catch(e){authView(false);alertText(e.message);}
  }
  async function completeOwnerLogin(){
    const session=await request("/session");
    csrf=session.authenticated?(session.csrf||""):"";
    if(!csrf)throw new Error("未取得安全写入令牌，请刷新后重试。");
    await load();
    $("subEmailResetSection").classList.toggle("hidden",!session.email_reset_allowed);
    $("subEmailLogin").classList.add("hidden");
    $("subEmailToggle").classList.add("hidden");
  }
  $("subEmailToggle").addEventListener("click",()=>{
    $("subLogin").classList.add("hidden");
    $("subEmailToggle").classList.add("hidden");
    $("subEmailLogin").classList.remove("hidden");
    clearAlert();
  });
  $("subEmailBack").addEventListener("click",()=>{
    $("subEmailLogin").classList.add("hidden");
    $("subLogin").classList.remove("hidden");
    $("subEmailToggle").classList.remove("hidden");
    clearAlert();
  });
  $("subEmailSend").addEventListener("click",async()=>{
    const button=$("subEmailSend");
    if(button.disabled)return;
    button.disabled=true;
    clearAlert();
    try{
      await request("/email/request",{method:"POST",headers:{"Content-Type":"application/json"},body:"{}"});
      alertText("邮件已请求发送。请查看预绑定邮箱（包括垃圾邮件）。验证码 10 分钟内有效。",true);
    }catch(e){alertText(e.message);}
    finally{button.disabled=false;}
  });
  $("subEmailVerifyForm").addEventListener("submit",async event=>{
    event.preventDefault();clearAlert();
    const button=$("subEmailVerify");
    if(button.disabled)return;
    button.disabled=true;
    try{
      await request("/email/verify",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({code:$("subEmailCode").value.trim()})});
      $("subEmailCode").value="";
      await completeOwnerLogin();
      alertText("邮箱验证成功，已登录订阅中心。",true);
    }catch(e){alertText(e.message);}
    finally{button.disabled=false;}
  });
  $("subEmailResetForm").addEventListener("submit",async event=>{
    event.preventDefault();clearAlert();
    const pass=$("subEmailNewPassword").value;
    if(pass!==$("subEmailNewPasswordConfirm").value){alertText("两次密码不一致");return;}
    if(pass.length<12||pass.length>128){alertText("密码必须为 12–128 字符");return;}
    const button=$("subEmailResetSubmit");
    if(button.disabled)return;
    button.disabled=true;
    try{
      await request("/email/reset-password",{method:"PUT",body:JSON.stringify({password:pass})});
      $("subEmailNewPassword").value="";$("subEmailNewPasswordConfirm").value="";
      $("subEmailResetSection").classList.add("hidden");
      csrf="";items=[];closePanels();authView(false);
      $("subLogin").classList.remove("hidden");
      alertText("订阅密码已重设，原登录会话均已失效，请使用新密码登录。",true);
    }catch(e){alertText(e.message);}
    finally{button.disabled=false;}
  });
  $("subLogin").addEventListener("submit",async event=>{
    event.preventDefault();clearAlert();
    const password=$("subPassword").value;
    try {
      await request("/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({password})});
      $("subPassword").value="";
      await completeOwnerLogin();
    }catch(e){alertText(e.message);}
  });
  $("subLogout").addEventListener("click",async()=>{
    if(accessMode){
      // Cloudflare Access owns this session; don't claim local logout succeeded.
      window.location.assign("/cdn-cgi/access/logout");
      return;
    }
    try{await request("/logout",{method:"POST"});}
    catch(e){alertText(e.message);return;}
    items=[];csrf="";closePanels();$("subEmailResetSection").classList.add("hidden");authView(false);
  });
  for(const id of ["subQuery","subCategory","subStatus"])$(id).addEventListener(id==="subQuery"?"input":"change",render);
  const currentYear=new Date().getFullYear();
  for(let y=currentYear-1;y<=currentYear+2;y++){const option=el("option","",y);option.value=String(y);$("subYear").append(option);}
  $("subYear").value=String(currentYear);
  $("subCalendarMonth").value=String(new Date().getMonth()+1);
  for(const id of ["subYear","subCalendarMonth"])$(id).addEventListener("change",renderInsights);
  $("subAdd").addEventListener("click",()=>openEditor());
  $("subEditCycle").addEventListener("change",refreshCycle);
  for(const id of ["subEditCancel","subEditCancelTop","subRenewCancel","subRenewCancelTop"])
    $(id).addEventListener("click",closePanels);
  $("subEditForm").addEventListener("submit",async event=>{
    event.preventDefault();
    if(busy)return;
    try{
      const payload=subscriptionPayload();
      await mutate("/manage",editingID?"PUT":"POST",payload,editingID?"订阅已更新。":"订阅已添加。",$("subEditSave"));
    }catch(e){alertText(e.message);}
  });
  $("subRenewForm").addEventListener("submit",async event=>{
    event.preventDefault();
    if(busy||!renewingID)return;
    const item=items.find(x=>x.id===renewingID);
    if(!item){alertText("订阅记录已变化，请刷新页面。");return;}
    try{
      const currency=$("subRenewCurrency").value.trim().toUpperCase();
      const data={
        subscription_id:renewingID,
        date:$("subRenewDate").value,
        amount_minor:minorFromInput($("subRenewAmount").value,currency),
        currency,
        new_due_date:item.source==="manual"&&item.cycle!=="long_term"?$("subRenewDue").value:"",
        notes:$("subRenewNotes").value.trim()
      };
      await mutate("/renewals","POST",data,"续费记录已保存。",$("subRenewSave"));
    }catch(e){alertText(e.message);}
  });
  init();
})();