"use strict";
const fs = require("node:fs");
const assert = require("node:assert/strict");

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
const pages = ["index.html", "network.html", "traffic.html", "subscriptions.html"];
for (const file of pages) {
  const content = read("cmd/miniprobe-server/web/" + file);
  const refs = [...content.matchAll(/\/ui\/([0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?)\//g)];
  assert.ok(refs.length, file + " missing versioned UI assets");
  for (const [, refVersion] of refs) {
    assert.equal(refVersion, version, file + " stale UI asset reference");
  }
}
const changelog = "CHANGELOG-v" + version + ".md";
assert.ok(fs.statSync(changelog).isFile(), "Missing " + changelog);
console.log("Release version consistency verified: v" + version);
