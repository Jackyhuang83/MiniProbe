# MiniProbe v0.4.16

## Stable release

- 将已经过实际部署验证的 `v0.4.16-alpha` 收口为 `v0.4.16` 正式版。
- 不新增功能、不改变数据结构、不重建线路历史或 ASN 基准。
- 保留版本化 Dashboard 路径 `/ui/<Server版本>/...`，避免 Cloudflare / 浏览器继续命中旧静态资源。
- 保留 `/ui/<Server版本>/` 根路径重定向循环修复。
- 保留 CN2 / 163、CUII / 9929 / 4837、CMIN2 / CMI / CMNET 可读线路识别。

## Compatibility

- Server 版本：`v0.4.16`。
- 目标 Agent 版本继续保持 `v0.4.12-alpha`；Agent 探测代码未变化，无需为了正式版标签重复升级现有 Agent。
- 节点数据、流量累计、线路历史、ASN 基准、Dashboard 登录状态与 `v0.4.16-alpha` 完全兼容。

## Release status

- 本版本作为当前正式稳定基线。后续除非发现 Bug 或有明确新需求，不主动扩展功能。
