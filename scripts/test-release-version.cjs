"use strict";
const fs = require("node:fs");
const assert = require("node:assert/strict");
const {execFileSync} = require("node:child_process");

function read(path) {
  return fs.readFileSync(path, "utf8");
}
function capture(value, regex, name) {
  const match = value.match(regex);
  assert.ok(match, "Missing release version marker in " + name);
  return match[1];
}
const make = read("Makefile");
const version = capture(make, /^VERSION \?= ([^\r\n]+)$/m, "Makefile");
const server = read("cmd/miniprobe-server/main.go");
assert.equal(capture(server, /serverVersion\s*=\s*"([^"]+)"/, "serverVersion"), version);

const installer = read("scripts/install-server.sh");
assert.equal(capture(installer, /VERSION=\$\{MINIPROBE_VERSION:-v([^}]+)\}/, "install-server.sh"), version);

const jsFiles = ["app.js", "network.js", "traffic.js"];
for (const file of jsFiles) {
  const content = read("cmd/miniprobe-server/web/" + file);
  assert.equal(capture(content, /const UI_VERSION='([^']+)'/, file), version);
}
const pages = ["index.html", "network.html", "traffic.html", "subscriptions.html", "clipboard.html"];
for (const file of pages) {
  const content = read("cmd/miniprobe-server/web/" + file);
  const refs = [...content.matchAll(/\/ui\/([0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?)\//g)];
  assert.ok(refs.length, file + " missing versioned UI assets");
  for (const [, refVersion] of refs) {
    assert.equal(refVersion, version, file + " stale UI asset reference");
  }
}
execFileSync("node", ["--check", "cmd/miniprobe-server/web/clipboard.js"], {stdio:"inherit"});
const clipboard = read("cmd/miniprobe-server/web/clipboard.html");
assert.match(clipboard, /24 小时自动清理与手动删除/, "clipboard must show retention/deletion notice");
assert.match(clipboard, /服务器管理员或服务器被入侵/, "clipboard must show sensitive-data notice");
assert.match(clipboard, /不写入数据库，也不会纳入 MiniProbe 每日自动备份/, "clipboard must clarify memory-only storage");
assert.match(clipboard, /其他设备刷新页面将无法再查看该内容/, "clipboard must clarify deletion visibility");
assert.match(clipboard, /独立副本，这些副本不会随之删除/, "clipboard must clarify independent copies");
const changelog = "CHANGELOG-v" + version + ".md";
assert.ok(fs.statSync(changelog).isFile(), "Missing " + changelog);
console.log("Release version consistency verified: v" + version);
