"use strict";

(() => {
  const byId = (id) => document.getElementById(id);
  const ns = "http://www.w3.org/2000/svg";
  const roleOrder = ["entry", "relay", "exit"];
  const roleNames = {entry:"入口", relay:"中转", exit:"出口"};
  const linkPalette = ["#58a1d2","#c48af6","#ebaf72","#50c5b0","#cf88a3","#a1bb69"];
  const networkNames={healthy:"网络正常",degraded:"网络降级",interrupted:"网络中断",stale:"数据过期",pending:"尚未探测"};
  const probeNames={not_configured:"未配置目标",agent_unsupported:"来源 Agent 暂不支持",pending:"等待首轮探测",measured:"已取得定向探测结果"};
  const ms=(v)=>v===null || v===undefined?"—":Number(v).toFixed(1)+" ms";
  const pct=(v)=>v===null || v===undefined?"—":Number(v).toFixed(1)+"%";
  const trafficNames={not_collected:"未采集",unsupported:"来源 Agent 暂不支持",pending:"等待计数器数据",baseline:"首次基线建立中",collected:"已采集",stale:"数据过期"};
  const volume=(v)=>v===null || v===undefined?"未采集":(Number(v)/1e9).toFixed(3)+" GB";
  const speed=(v)=>v===null || v===undefined?"未采集":(Number(v)/1e6).toFixed(3)+" Mbps";
  const when=(v)=>!v || v.startsWith("0001")?"—":new Date(v).toLocaleString();
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
  let selectedHistoryLink = "";
  async function showLinkHistory(link, target) {
    if(link.traffic_status==="not_collected" || link.traffic_status==="unsupported") return;
    const line=element("div","读取最近 7 天历史…","small");
    target.append(line);
    try {
      const res=await fetch("/api/v1/topology/traffic/history?days=7",{credentials:"same-origin",cache:"no-store"});
      if(!res.ok) throw new Error("HTTP "+res.status);
      const history=await res.json();
      if(selectedHistoryLink!==link.id) return;
      const days=[];
      for (const day of history.days||[]) {
        const item=(day.links||[]).find(item=>item.id===link.id);
        if(item) days.push(day.date+" (UTC)：上行 "+volume(item.uplink_bytes)+
          " / 下行 "+volume(item.downlink_bytes)+(item.incomplete?" · 有缺口":""));
      }
      line.replaceChildren(element("strong","最近 7 天 · 链路流量历史（UTC）"));
      if(days.length===0) line.append(element("div","暂无有效历史检查点","small"));
      else days.forEach(row=>line.append(element("div",row,"small")));
    }catch(e){
      if(selectedHistoryLink===link.id)line.textContent="历史数据暂不可读取："+e.message;
    }
  }
  function showLink(l) {
    selectedHistoryLink=l.id;
    const nodes = new Map(data.nodes.map((n)=>[n.id,n]));
    const start = nodes.get(l.from), end = nodes.get(l.to);
    const detail = byId("topologyDetail");
    detail.replaceChildren();
    detail.append(element("strong",(start ? start.name : l.from) + " → " + (end ? end.name : l.to)),
      row("备注",l.label || "无"),
      row("逻辑关系","已配置"),
      row("端点状态",l.endpoint_status === "online" ? "两端 Agent 均在线" : "至少一端 Agent 离线"),
      row("探测配置",probeNames[l.probe_state]||"未知"),
      row("定向网络状态",networkNames[l.network_status]||"未知"),
      row("网络可达性",l.reachability==="reachable"?"TCP 可连接":l.reachability==="unreachable"?"TCP 无法连接":l.reachability==="stale"?"结果已过期":"尚未验证"),
      row("最近 RTT",ms(l.rtt_ms)),
      row("最低 / 最高 RTT",ms(l.min_ms)+" / "+ms(l.max_ms)),
      row("历史平均 RTT",ms(l.avg_ms)),
      row("最近 20 次探测窗口失败率",pct(l.loss_pct)),
      row("最近成功",when(l.last_success)),
      row("最近故障",when(l.last_failure)),
      row("最近一次探测",when(l.last_probe)),
      row("代理协议健康","未测试：TCP 握手成功不等于代理业务可用"),
      row("链路流量采集",trafficNames[l.traffic_status]||"未采集"),
      row("今日上行 / 下行（UTC）",volume(l.today_uplink_bytes)+" / "+volume(l.today_downlink_bytes)),
      row("实时上行 / 下行",speed(l.uplink_bps)+" / "+speed(l.downlink_bps)),
      row("最近流量样本",when(l.traffic_last_sample)),
      row("历史完整度",l.traffic_incomplete?"存在采集缺口，不可视为完整账单":"仍以首次基线之后的有效区间为准"));
    showLinkHistory(l,detail);
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
      const color = l.endpoint_status==="endpoint_offline" ? "#677789" :
        l.network_status==="interrupted" ? "#e56b72" : linkPalette[index%linkPalette.length];
      const solid=l.reachability==="reachable"||l.reachability==="unreachable";
      paths.append(svgElement("path",{d:curve,class:"topology-edge"+(l.endpoint_status==="endpoint_offline"?" endpoint-offline":""),style:"stroke:"+color+";stroke-dasharray:"+(solid?"none":"7 6")}));
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
      b.append(element("span",(l.label?l.label+" · ":"")+
        (l.probe_state==="measured"?(networkNames[l.network_status]||"已探测"):(probeNames[l.probe_state]||"尚未探测"))+
        " · "+(trafficNames[l.traffic_status]||"未采集")+
        " · 代理协议未验证"));
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
    byId("topologyTraffic").textContent=volume(s.traffic_today_bytes);
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
