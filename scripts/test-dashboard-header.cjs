'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

const source = fs.readFileSync(path.join(__dirname, '../cmd/miniprobe-server/web/app.js'), 'utf8');
assert.match(source, /\nboot\(\);\s*$/);
const document = {querySelector: () => ({addEventListener(){}})};
const context = vm.createContext({document});
vm.runInContext(source.replace(/\nboot\(\);\s*$/, ''), context, {filename:'app.js'});

function count(nodes){
  context.__fixture=nodes;
  return JSON.parse(JSON.stringify(vm.runInContext('headerCounts(__fixture)',context)));
}
function markup(nodes){
  context.__fixture=nodes;
  return vm.runInContext('headerSummaryMarkup(__fixture)',context);
}
// VPS billing price uses the actual selected payment period; older nodes default monthly.
context.__month={monthly_price:10,currency:'USD'};
context.__year={monthly_price:120,currency:'USD',billing_cycle:'yearly'};
assert.equal(vm.runInContext('priceText(__month)',context),'$10/月');
assert.equal(vm.runInContext('priceText(__year)',context),'$120/年');
const nodes = [
  ...Array.from({length:4},(_,i)=>({display_name:'HK-node-'+i,online:true})),
  {tags:['JP'],display_name:'Tokyo-A',online:true},
  {display_name:'JP-2',online:true},
  {tags:['TW'],display_name:'Taipei-A',online:true},
  {display_name:'TW-2',online:true},
  {tags:['US'],display_name:'US-A',online:true},
  {display_name:'US-2',online:true}
];
assert.deepEqual(count(nodes),{total:10,online:10,HK:4,JP:2,TW:2,US:2,other:0});
const html=markup(nodes);
for(const token of ['节点 <strong>10</strong>','在线 <strong>10</strong>','HK <strong>4台</strong>','JP <strong>2台</strong>','TW <strong>2台</strong>','US <strong>2台</strong>','其它 <strong>0台</strong>']) {
  assert.ok(html.includes(token),token);
}
assert.deepEqual(count([...nodes,{tags:['SG'],online:false},{display_name:'Unknown VPS',online:false}]),{total:12,online:10,HK:4,JP:2,TW:2,US:2,other:2});
assert.deepEqual(count([]),{total:0,online:0,HK:0,JP:0,TW:0,US:0,other:0});
assert.deepEqual(count([{tags:['HK'],online:false},{tags:['JP'],online:true}]),{total:2,online:1,HK:1,JP:1,TW:0,US:0,other:0});

const page=fs.readFileSync(path.join(__dirname,'../cmd/miniprobe-server/web/index.html'),'utf8');
const css=fs.readFileSync(path.join(__dirname,'../cmd/miniprobe-server/web/style.css'),'utf8');
assert.match(page, /class="dashboard-header-main"/);
assert.match(page, /class="dashboard-header-summary"/);
assert.match(page, /id="summary" class="header-stats"/);
assert.match(css, /\.dashboard-header\{flex-direction:column/);
assert.match(css, /\.header-stats\{[^}]*flex-wrap:wrap/);
console.log('Dashboard two-row header and regional node summary: ok');
