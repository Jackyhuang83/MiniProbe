"use strict";

(() => {
  const byId = (id) => document.getElementById(id);
  const ns = "http://www.w3.org/2000/svg";
  const roleOrder = ["entry", "relay", "exit"];
  const roleNames = {entry:"入口", relay:"中转", exit:"出口"};
  const linkPalette = ["#58a1d2","#c48af6","#ebaf72","#50c5b0","#cf88a3","#a1bb69"];
  let data = null;
  let grouped = false;

  function element(tag, text, cls) {
    const e = document.createElement(tag);
    if (text !== undefined) e.textContent = String(text);
    if (cls) e.className = cls;
    return e;
  }
  function svgElement(tag, attrs, text) {
    const e = document.createElementNS(ns,tag);
    for (const [k,v] of Object.entries(attrs || {})) e.setAttribute(k,String(v));
    if (text !== undefined) e.textContent = String(text);
    return e;
  }
  function row(key,value) {
    const e = element("div",undefined,"topology-detail-row");
    e.append(element("b",key),element("span",value));
    return e;
  }
  function showNode(n) {
    const detail = byId("topologyDetail");
    detail.replaceChildren();
    detail.append(element("strong",n.name),row("角色",roleNames[n.role] || n.role),
      row("分组",n.group || "未分组"),row("Agent 在线",n.online ? "在线" : "离线"),
      row("Agent 版本",n.agent_version || "未上报"),
      row("系统 / 架构",(n.os || "未知") + " / " + (n.arch || "未知")),
      row("网络类型",(n.network_types || []).join("、") || "未知"),
      row("最后上报",n.last_seen && !n.last_seen.startsWith("0001") ? new Date(n.last_seen).toLocaleString() : "尚未上报"));
  }
  function showLink(l) {
    const nodes = new Map(data.nodes.map((n)=>[n.id,n]));
    const start = nodes.get(l.from), end = nodes.get(l.to);
    const detail = byId("topologyDetail");
    detail.replaceChildren();
    detail.append(element("strong",(start ? start.name : l.from) + " → " + (end ? end.name : l.to)),
      row("备注",l.label || "无"),
      row("逻辑关系","已配置"),
      row("端点状态",l.endpoint_status === "online" ? "两端 Agent 均在线" : "至少一端 Agent 离线"),
      row("定向 RTT","未探测（DEV2）"),
      row("丢包 / 失败率","未探测（DEV2）"),
      row("网络可达性","尚未验证"),
      row("代理协议健康","尚未验证"),
      row("今日转发流量","未采集（DEV3）"),
      row("实时流量","未采集（DEV3）"));
  }
  function layout(nodes) {
    const positions = new Map();
    const labels = [];
    let height = 320;
    for (let col=0;col<3;col++) {
      const role = roleOrder[col], items = nodes.filter((n)=>n.role===role).slice();
      items.sort((a,b)=>a.group.localeCompare(b.group,"zh-CN") || a.name.localeCompare(b.name,"zh-CN"));
      let y = 100, previous = null;
      for (const n of items) {
        if (grouped && n.group !== previous) {
          labels.push({x:34+col*332,y:y-8,label:n.group || "未分组"});
          y += 23;
        }
        positions.set(n.id,{x:34+col*332,y:y,n});
        y += 78;
        previous = n.group;
      }
      height=Math.max(height,y+75);
    }
    return {positions,labels,height};
  }
  function renderCanvas() {
    const target = byId("topologyCanvas");
    target.replaceChildren();
    const {positions,labels,height} = layout(data.nodes);
    const svg=svgElement("svg",{viewBox:"0 0 1000 "+height,role:"img","aria-label":"VPS 有向逻辑拓扑图"});
    const headings=svgElement("g");
    roleOrder.forEach((role,i)=>headings.append(svgElement("text",{x:34+i*332,y:45,class:"topology-role"},roleNames[role]+"节点")));
    svg.append(headings);
    const paths=svgElement("g");
    for (const [index,l] of data.links.entries()) {
      const a=positions.get(l.from),b=positions.get(l.to);
      if(!a || !b)continue;
      const x1=a.x+235,y1=a.y+27,x2=b.x,y2=b.y+27;
      const bend=Math.max(65,Math.abs(x2-x1)*.42);
      const curve="M "+x1+" "+y1+" C "+(x1+bend)+" "+y1+" "+(x2-bend)+" "+y2+" "+x2+" "+y2;
      const color = l.endpoint_status==="endpoint_offline" ? "#677789" : linkPalette[index%linkPalette.length];
      paths.append(svgElement("path",{d:curve,class:"topology-edge"+(l.endpoint_status==="endpoint_offline"?" endpoint-offline":""),style:"stroke:"+color}));
      const hit=svgElement("path",{d:curve,class:"topology-edge-hit",tabindex:"0",role:"button","aria-label":"查看逻辑链路 "+l.from+" 到 "+l.to});
      hit.addEventListener("click",()=>showLink(l));
      hit.addEventListener("keydown",(ev)=>{if(ev.key==="Enter" || ev.key===" "){ev.preventDefault();showLink(l);}});
      paths.append(hit);
    }
    svg.append(paths);
    const groups=svgElement("g");
    for (const g of labels)groups.append(svgElement("text",{x:g.x,y:g.y,class:"topology-group"},g.label));
    svg.append(groups);
    const layer=svgElement("g");
    for(const n of data.nodes) {
      const pos=positions.get(n.id);
      if(!pos)continue;
      const g=svgElement("g",{class:"topology-node"+(n.online?"":" topology-node-offline"),tabindex:"0",role:"button","aria-label":"查看节点 "+n.name});
      const x=pos.x,y=pos.y;
      g.append(svgElement("rect",{x,y,width:235,height:56,rx:9}));
      g.append(svgElement("circle",{cx:x+16,cy:y+20,r:5,fill:n.online?"#13d6a1":"#8794a4"}));
      const name=n.name.length>22?n.name.slice(0,21)+"…":n.name;
      g.append(svgElement("text",{x:x+30,y:y+24,"font-size":13,"font-weight":650},name));
      g.append(svgElement("text",{x:x+14,y:y+44,class:"topology-node-sub"},(n.group||"默认分组")+" · "+(n.online?"在线":"离线")));
      g.append(svgElement("title",{},n.name));
      g.addEventListener("click",()=>showNode(n));
      g.addEventListener("keydown",(ev)=>{if(ev.key==="Enter"||ev.key===" "){ev.preventDefault();showNode(n);}});
      layer.append(g);
    }
    svg.append(layer);
    target.append(svg);
    if(data.nodes.length===0)target.append(element("p","尚未添加拓扑节点，请从 SSH 中文菜单配置。","topology-footnote"));
  }
  function renderList() {
    const list=byId("topologyLinks");list.replaceChildren();
    const names = new Map(data.nodes.map(n=>[n.id,n.name]));
    if(data.links.length===0)list.append(element("div","尚未配置逻辑链路","small"));
    for(const l of data.links){
      const b=element("button",(names.get(l.from)||l.from)+" → "+(names.get(l.to)||l.to),"topology-link-button");
      b.type="button";
      b.append(element("span",(l.label?l.label+" · ":"")+"已配置 · 尚未定向探测 · 转发流量未采集"));
      b.addEventListener("click",()=>showLink(l));
      list.append(b);
    }
  }
  function render() {
    byId("topologyDisabled").classList.toggle("hidden",data.enabled);
    byId("topologyView").classList.toggle("hidden",!data.enabled);
    if(!data.enabled) return;
    const s=data.summary;
    byId("topologyHealthy").textContent=(s.healthy_links===null?"未探测":String(s.healthy_links))+" / "+s.total_links;
    byId("topologyTraffic").textContent=s.traffic_today_bytes===null?"未采集":s.traffic_today_bytes.toLocaleString()+" B";
    byId("topologyLatency").textContent=s.average_rtt_ms===null?"未探测":s.average_rtt_ms.toFixed(1)+" ms";
    renderCanvas();renderList();
  }
  async function load() {
    try {
      const res=await fetch("/api/v1/topology",{credentials:"same-origin",cache:"no-store"});
      if(!res.ok)throw new Error(res.status===401?"登录会话失效，请返回 Dashboard 登录":"读取拓扑失败 (HTTP "+res.status+")");
      data=await res.json();
      render();
      byId("topologyError").classList.add("hidden");
      byId("topologyRefresh").textContent="更新于 "+new Date().toLocaleTimeString();
    } catch(e) {
      byId("topologyError").textContent=e.message;
      byId("topologyError").classList.remove("hidden");
      byId("topologyRefresh").textContent="读取失败";
    }
  }
  function chooseGroup(next){
    grouped=next;
    for(const [id,selected] of [["topologyByRole",!next],["topologyByGroup",next]]){
      byId(id).classList.toggle("active",selected);
      byId(id).setAttribute("aria-pressed",String(selected));
    }
    if(data && data.enabled)renderCanvas();
  }
  byId("topologyByRole").addEventListener("click",()=>chooseGroup(false));
  byId("topologyByGroup").addEventListener("click",()=>chooseGroup(true));
  load();
  setInterval(()=>{if(!document.hidden)load();},30000);
})();
