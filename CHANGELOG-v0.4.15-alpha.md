# MiniProbe v0.4.15-alpha

## Fixed

- 修复 `v0.4.14-alpha` 仍可能被 Cloudflare 旧缓存命中的问题：部分缓存规则会忽略 Query String，因此 `?v=<版本>` 不能作为绝对可靠的缓存隔离键。
- Dashboard 改用版本化路径 `/ui/<Server版本>/...`。HTML、JS、CSS 和系统图标的 URL 路径本身随版本变化，不依赖 Query String。
- 未版本化的 `/`、`/index.html`、`/network.html` 在到达源站后统一 `307` 到当前 `/ui/<版本>/...`，并保留节点 `id` 等查询参数。
- `/ui/<当前版本>/` 下的 HTML 返回 `no-store`；JS/CSS/SVG 使用长期 `immutable` 缓存。
- Dashboard Session 继续返回 `server_version`；从已进入版本化 UI 后，后续 Server/UI 升级会自动跳到新的 `/ui/<版本>/...` 路径。

## Compatibility

- Server 版本提升到 `v0.4.15-alpha`。
- 目标 Agent 版本继续保持 `v0.4.12-alpha`；不修改 Agent、RTT、traceroute、流量或上报协议，无需升级 Agent。
- 保留 CN2 / 163、CUII / 9929 / 4837、CMIN2 / CMI / CMNET 线路识别及现有历史/ASN 基准。

## Upgrade note

- 如果 Cloudflare 当前仍持有 `v0.4.13` 之前的旧根页面缓存，不需要 Purge。升级 Server 后首次直接访问全新的 `/ui/0.4.15-alpha/` 路径即可绕过旧缓存；进入本版后，后续版本由 UI 的 Server 版本检测自动迁移。
