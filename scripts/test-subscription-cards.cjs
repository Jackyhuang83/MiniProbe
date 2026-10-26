"use strict";
// Exercise the actual browser card renderer without Node dependencies or network.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");
class FakeElement {
  constructor(tag) {
    this.tag=tag; this.className=""; this.textContent=""; this.value="";
    this.children=[]; this.listeners={};
    this.classList={add(){},remove(){},toggle(){}};
  }
  append(...nodes){this.children.push(...nodes)}
  replaceChildren(...nodes){this.children=nodes}
  addEventListener(type,handler){this.listeners[type]=handler}
}
const elements=new Map();
const document={
  createElement:tag=>new FakeElement(tag),
  getElementById:id=>{
    if(!elements.has(id))elements.set(id,new FakeElement("div"));
    return elements.get(id);
  }
};
const data=[
  {source:"manual",name:"年度影视",category:"video",amount_minor:12000,currency:"CNY",cycle:"yearly",auto_renew:true,status:"active",due_date:"2030-01-01",time_zone:"Asia/Tokyo"},
  {source:"manual",name:"月度 AI",category:"ai",amount_minor:2000,currency:"USD",cycle:"monthly",auto_renew:false,status:"active",due_date:"2030-01-01",time_zone:"Asia/Tokyo"},
  {source:"manual",name:"九十天方案",category:"software",amount_minor:9900,currency:"USD",cycle:"custom",custom_days:90,auto_renew:false,status:"active",due_date:"2030-01-01",time_zone:"Asia/Tokyo"},
  {source:"vps",node_id:"node-a",name:"真实 VPS",category:"vps",amount_minor:1000,currency:"USD",cycle:"monthly",auto_renew:false,status:"active",due_date:"2030-01-01",time_zone:"Asia/Tokyo"},
  {source:"vps",node_id:"node-b",name:"年付 VPS",category:"vps",amount_minor:12000,currency:"USD",cycle:"yearly",auto_renew:false,status:"active",due_date:"2030-01-01",time_zone:"Asia/Tokyo"}
];
const fetch=async url=>({ok:true,json:async()=>url.endsWith("/session")?{secure:true,enabled:true,authenticated:true}:{items:data}});
const source=fs.readFileSync(path.join(__dirname,"..","cmd","miniprobe-server","web","subscriptions.js"),"utf8");
vm.runInNewContext(source,{document,fetch,Date,Intl});
const flattened=el=>String(el.textContent)+(el.children||[]).map(flattened).join(" ");
const findClass=(el,name)=>el.className.split(" ").includes(name)||(el.children||[]).some(child=>findClass(child,name));
(async()=>{
  for(let n=0;n<4;n++) await new Promise(resolve=>setImmediate(resolve));
  const cards=elements.get("subCards").children;
  assert.equal(cards.length,5,"subscription cards loaded");
  assert.match(flattened(cards[0]),/年度/);
  assert.match(flattened(cards[0]),/自动续费/);
  assert.ok(findClass(cards[0],"yearly"));
  assert.match(flattened(cards[1]),/月度/);
  assert.match(flattened(cards[1]),/手动续费/);
  assert.ok(findClass(cards[1],"monthly"));
  assert.match(flattened(cards[2]),/自定义 · 90天/);
  assert.match(flattened(cards[3]),/月租/);
  assert.match(flattened(cards[3]),/续费方式未设置/);
  assert.doesNotMatch(flattened(cards[3]),/手动续费|自动续费/,"never invent VPS renewal mode");
  assert.match(flattened(cards[3]),/节点月租金额/);
  assert.match(flattened(cards[4]),/年付 VPS/);
  assert.match(flattened(cards[4]),/年付/);
  assert.match(flattened(cards[4]),/节点年付金额/);
  assert.doesNotMatch(flattened(cards[4]),/月租金额|自动续费|手动续费/);
  // Verify first-time setup explains Docker container menus accurately.
  const setupElements=new Map();
  const setupDocument={
    createElement:tag=>new FakeElement(tag),
    getElementById:id=>{
      if(!setupElements.has(id))setupElements.set(id,new FakeElement("div"));
      return setupElements.get(id);
    }
  };
  const setupFetch=async()=>({ok:true,json:async()=>({secure:true,enabled:false,authenticated:false})});
  vm.runInNewContext(source,{document:setupDocument,fetch:setupFetch,Date,Intl});
  for(let n=0;n<3;n++) await new Promise(resolve=>setImmediate(resolve));
  assert.match(setupElements.get("subGateText").textContent,/Docker 版请进入容器内 MiniProbe 管理菜单 → 15 → 6/);
  assert.doesNotMatch(setupElements.get("subGateText").textContent,/Server SSH/);
  const html=fs.readFileSync(path.join(__dirname,"..","cmd","miniprobe-server","web","subscriptions.html"),"utf8");
  assert.match(html,/miniprobe → 15 → 6/,"show the short SSH management entrypoint");
  assert.match(html,/15 → 11/,"show local SMTP recovery setup route");
  assert.match(html,/忘记密码？使用邮箱一次性验证码/,"show optional email recovery");

  assert.doesNotMatch(html,/Server SSH/);
  console.log("Subscription card and Docker first-time setup guidance: OK");
})().catch(err=>{console.error(err);process.exitCode=1});
