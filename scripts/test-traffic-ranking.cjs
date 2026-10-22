'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const code = fs.readFileSync(path.join(__dirname, '../cmd/miniprobe-server/web/traffic.js'), 'utf8');
const elements = new Map();
const document = {
  querySelectorAll: () => [],
  querySelector: (selector) => {
    if (!elements.has(selector)) elements.set(selector, {value:'',innerHTML:'',addEventListener(){}});
    return elements.get(selector);
  },
};
const ctx = vm.createContext({document});
// Prevent network/UI boot; execute exactly the same ranking functions as the browser.
assert.match(code, /\nboot\(\);\s*$/);
vm.runInContext(code.replace(/\nboot\(\);\s*$/, ''), ctx, {filename:'traffic.js'});
function call(name,arg){ctx.__arg=arg;return vm.runInContext(`${name}(__arg)`,ctx);}
const live={quota_bytes:1000,cycle_start:'2026-10-01T00:00:00Z'};
for(const [percent,want] of [[0,'good'],[59.9,'good'],[59.99,'good'],[60,'warning'],[79.9,'warning'],[80,'warning'],[89.9,'warning'],[90,'warning'],[90.01,'danger'],[90.1,'danger'],[115,'danger']]){
  const found=call('rankingUsageInfo',{...live,usage_percent:percent});
  assert.equal(found.tier,want,`incorrect risk tier: ${percent}%`);
}
assert.equal(call('rankingUsageInfo',{quota_bytes:0,usage_percent:90,cycle_start:live.cycle_start}).percent,null);
assert.equal(call('rankingUsageInfo',{quota_bytes:1000,usage_percent:0}).percent,null);
const over=call('renderRankingUsage',{...live,usage_percent:115});
assert.match(over,/115\.0%/);
assert.match(over,/width:100\.0%/);
assert.match(over,/traffic-usage-danger/);
function expiry(s,y=2026,m=10,d=22){
  ctx.__expiry=s;ctx.__date=[y,m,d];
  return vm.runInContext('rankingExpiryInfo(__expiry,new Date(__date[0],__date[1]-1,__date[2],12))',ctx);
}
assert.equal(expiry('2026-10-29').note,'剩余 7 天');
assert.equal(expiry('2026-10-23').note,'剩余 1 天');
assert.equal(expiry('2026-10-22').note,'今天到期');
assert.equal(expiry('2026-10-20').note,'已到期 2 天');
assert.equal(expiry('2026-11-01').note,'剩余 10 天');
assert.equal(expiry('2026-03-09',2026,3,7).note,'剩余 2 天'); // DST-safe date arithmetic
assert.equal(expiry('2036-01-01').date,'长期');
assert.equal(expiry('longterm').date,'长期');
assert.equal(expiry('').note,'未设置到期日');
assert.equal(expiry('2026-02-30').note,'到期日无效');
ctx.__fixture={nodes:[
  {id:'node-a',display_name:'Tokyo <script>',online:true,quota_bytes:1000,cycle_used_bytes:900,remaining_bytes:100,usage_percent:90,cycle_start:live.cycle_start,expire_at:'2026-11-20',history:{used_bytes:20,inbound_bytes:30,outbound_bytes:40}},
  {id:'node-b',display_name:'Long',online:false,quota_bytes:0,expire_at:'2036-01-01',history:{used_bytes:10,inbound_bytes:10,outbound_bytes:10}},
]};
vm.runInContext('current=__fixture;renderRanking();',ctx);
const html=elements.get('#trafficRanking').innerHTML;
assert.match(html,/<th>流量使用情况<\/th><th>订阅到期<\/th>/);
assert.match(html,/traffic-usage-warning/);
assert.doesNotMatch(html,/traffic-usage-orange/);
assert.match(html,/2026-11-20/);
assert.match(html,/traffic-expiry-longterm/);
assert.match(html,/未设置额度/);
assert.match(html,/Tokyo &lt;script&gt;/);
assert.doesNotMatch(html,/<script>/);
console.log('traffic ranking usage thresholds, expiry calendar and rendering: ok');
