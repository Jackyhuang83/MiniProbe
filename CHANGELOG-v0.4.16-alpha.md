# MiniProbe v0.4.16-alpha

## Fixed

- 修复 `v0.4.15-alpha` 版本化 Dashboard 根路径 `/ui/<Server版本>/` 的重定向循环。
- 根因：中间件把版本化根路径内部改写为 `/index.html` 后交给 Go `http.FileServer`；`FileServer` 会把 `/index.html` 规范化重定向到 `./`，浏览器解析后仍回到同一个 `/ui/<版本>/`，最终触发 `ERR_TOO_MANY_REDIRECTS`。
- 修复后 `/ui/<版本>/` 直接以内部分发路径 `/` 交给静态文件服务，由其正常读取 `index.html`，不再触发 `/index.html -> ./` 规范化跳转。
- `/ui/<版本>`（缺少末尾 `/`）只执行一次 `307` 到 `/ui/<版本>/`，作为唯一的根路径规范化跳转。
- 保留 v0.4.15-alpha 的版本化路径缓存隔离策略；无需清理 Cloudflare 缓存，也不再依赖 Query String。

## Regression coverage

- 新增真实嵌入式 `http.FileServer` 回归测试，验证 `/ui/<版本>/` 直接返回 `200` 且不会设置 `Location`。
- 验证 `/ui/<版本>` 只重定向一次到带 `/` 的版本化根路径。
- 保留旧 `/`、`/network.html?id=...` 到版本化路径的 `307` 行为，以及版本化 JS/CSS/SVG 的 `immutable` 缓存策略。

## Compatibility

- Server 版本提升到 `v0.4.16-alpha`。
- 目标 Agent 版本继续保持 `v0.4.12-alpha`；无需升级任何 Agent。
- 不修改节点数据、线路历史、ASN 基准、RTT、traceroute、流量统计或 Agent 上报协议。
