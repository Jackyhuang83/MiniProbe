# MiniProbe v0.4.14-alpha

## Fixed

- 修复 Cloudflare / 浏览器缓存旧 Dashboard 静态资源导致“Server 已升级但界面仍是上一版”的问题。
- Dashboard HTML 统一规范到带 `?v=<Server版本>` 的 URL，并保留节点 `id` 等原有查询参数。
- `index.html` / `network.html` 引用的 JS、CSS 和系统图标使用 Server 版本作为缓存键；带版本号的静态资源可安全长期缓存。
- 未带版本号的 HTML / JS / CSS / SVG 由源站返回 `no-store`，减少新的旧版本缓存条目。
- Dashboard Session 增加 `server_version`；从本版开始，若旧 UI 发现 Server 版本已变化，会自动跳转到对应版本 URL，避免后续发布再次要求手工清理 Cloudflare 缓存。

## Compatibility

- Server 版本提升到 `v0.4.14-alpha`。
- 目标 Agent 版本继续保持 `v0.4.12-alpha`；本版不修改 Agent、RTT、traceroute、流量或上报协议，不需要升级任何 Agent。
- 完整保留 v0.4.13-alpha 的 CN2 / 163、CUII / 9929 / 4837、CMIN2 / CMI / CMNET 线路识别。
- 现有线路历史、ASN 基准、路由变化事件和 Dashboard 登录状态兼容，不需要重建。
