"use strict";
// Browser-level DEV4 interaction test: no DOM framework or network required.
const assert=require("node:assert/strict");
const fs=require("node:fs");
const path=require("node:path");
const vm=require("node:vm");
class Element{
  constructor(tag){this.tag=tag;this.value="";this.checked=false;this.disabled=false;this.textContent="";this.className="";this.children=[];this.events={};this.required=false;this.classes=new Set();}
  get classList(){return {add:n=>this.classes.add(n),remove:n=>this.classes.delete(n),toggle:(n,force)=>{if(force===undefined? !this.classes.has(n):force)this.classes.add(n);else this.classes.delete(n);}}}
  append(...xs){this.children.push(...xs)}
  replaceChildren(...xs){this.children=xs}
  addEventListener(name,fn){this.events[name]=fn}
  focus(){}
  scrollIntoView(){}
}
const objects=new Map();
const document={
 createElement:tag=>new Element(tag),
 getElementById:id=>{
  if(!objects.has(id))objects.set(id,new Element("div"));
  return objects.get(id);
 }
};
const $=id=>document.getElementById(id);
const subscription={id:"sub-test",source:"manual",name:"Streaming",category:"video",amount_minor:1299,currency:"USD",cycle:"monthly",due_date:"2030-01-10",status:"active",time_zone:"Asia/Tokyo",auto_renew:true};
const vps={id:"vps:node",node_id:"node",source:"vps",name:"Node VPS",category:"vps",amount_minor:500,currency:"USD",cycle:"monthly",due_date:"2030-01-10",status:"active",time_zone:"Asia/Tokyo"};
let items=[subscription,vps];
const seen=[];
const csrf="ci-owner-csrf-123";
const fetch=async (url,opts={})=>{
 const method=opts.method||"GET";
 seen.push({url,method,options:opts});
 if(url.endsWith("/session"))return {ok:true,json:async()=>({secure:true,enabled:true,authenticated:true,csrf})};
 if(url==="/api/v1/subscriptions"&&method==="GET")return {ok:true,json:async()=>({items,renewals:[],reminders:{enabled:false,days:[]}})};
 if(["POST","PUT","DELETE"].includes(method)&&url.startsWith("/api/v1/subscriptions/")){
   assert.equal(opts.credentials,"same-origin");
   assert.equal(opts.headers["X-MiniProbe-Subscription-CSRF"],csrf);
   assert.equal(opts.headers["X-MiniProbe-Request"],"1");
   assert.equal(opts.headers["Content-Type"],"application/json");
   if(url.includes("/manage")){
     if(method==="DELETE")items=items.filter(x=>x.id!=="sub-test");
     else if(method==="PUT")items=[{...subscription,...JSON.parse(opts.body)},vps];
     else items=[...items,{...JSON.parse(opts.body),id:"sub-new",source:"manual"}];
   }
   return {ok:true,json:async()=>({ok:true})};
 }
 throw new Error("Unexpected request "+method+" "+url);
};
const window={confirm:()=>true};
const source=fs.readFileSync(path.join(__dirname,"..","cmd/miniprobe-server/web/subscriptions.js"),"utf8");
vm.runInNewContext(source,{document,window,fetch,Date,Intl});
const flush=async()=>{for(let i=0;i<8;i++)await new Promise(resolve=>setImmediate(resolve));};
function all(node){return [node,...(node.children||[]).flatMap(all)]}
function click(node){assert.ok(node.events.click);node.events.click()}
function submit(id){return $(id).events.submit({preventDefault(){}})}
(async()=>{
 await flush();
 const cards=$("subCards").children;
 assert.equal(cards.length,2,"manual + VPS cards render");
 const manualButtons=all(cards[0]).filter(n=>n.tag==="button").map(x=>x.textContent);
 const vpsButtons=all(cards[1]).filter(n=>n.tag==="button").map(x=>x.textContent);
 assert.deepEqual(manualButtons,["编辑","记录续费","删除"]);
 assert.deepEqual(vpsButtons,["记录付款"],"VPS cannot be edited or deleted");
 click(all(cards[0]).find(n=>n.textContent==="编辑"&&n.tag==="button"));
 assert.equal($("subEditName").value,"Streaming");
 $("subEditName").value="Streaming Plus";
 $("subEditAmount").value="14.52";
 await submit("subEditForm");await flush();
 const edit=seen.find(x=>x.url.endsWith("/manage")&&x.method==="PUT");
 assert.ok(edit,"edit request");
 assert.equal(JSON.parse(edit.options.body).amount_minor,1452);
 assert.equal(JSON.parse(edit.options.body).id,"sub-test");

 click($("subAdd"));
 $("subEditName").value="Music";
 $("subEditCategory").value="software";
 $("subEditAmount").value="1200";
 $("subEditCurrency").value="JPY";
 $("subEditCycle").value="yearly";
 $("subEditDue").value="2030-02-01";
 await submit("subEditForm");await flush();
 const create=seen.find(x=>x.url.endsWith("/manage")&&x.method==="POST");
 assert.ok(create,"create request");
 assert.equal(JSON.parse(create.options.body).amount_minor,1200);

 // Re-rendered cards are separate instances from the initial list.
 const renewedCard=$("subCards").children.find(card=>all(card).some(el=>el.tag==="strong"&&el.textContent==="Streaming Plus"));
 assert.ok(renewedCard);
 click(all(renewedCard).find(n=>n.tag==="button"&&n.textContent==="记录续费"));
 $("subRenewDate").value="2030-01-05";
 $("subRenewAmount").value="19.99";
 $("subRenewDue").value="2030-03-10";
 await submit("subRenewForm");await flush();
 const renewal=seen.find(x=>x.url.endsWith("/renewals")&&x.method==="POST");
 assert.ok(renewal);
 const record=JSON.parse(renewal.options.body);
 assert.equal(record.subscription_id,"sub-test");
 assert.equal(record.amount_minor,1999);
 assert.equal(record.new_due_date,"2030-03-10");

 const deleteCard=$("subCards").children.find(card=>all(card).some(el=>el.tag==="strong"&&el.textContent==="Streaming Plus"));
 click(all(deleteCard).find(n=>n.tag==="button"&&n.textContent==="删除"));
 await flush();
 assert.ok(seen.some(x=>x.url.includes("/manage?id=sub-test")&&x.method==="DELETE"));

 // The HTML must actually render the controls that the JS binds.
 const html=fs.readFileSync(path.join(__dirname,"..","cmd/miniprobe-server/web/subscriptions.html"),"utf8");
 for(const id of ["subAdd","subEditor","subEditForm","subEditSave","subRenewEditor","subRenewForm","subRenewSave"]){
   assert.ok(html.includes('id="'+id+'"'),"missing DEV4 element "+id);
 }
 assert.ok(!html.includes("不开放公网写入"),"outdated read-only claim");
 console.log("DEV4 web CRUD, renewal, VPS safety and CSRF header wiring: OK");
})().catch(err=>{console.error(err);process.exitCode=1});
