# MiniProbe v0.4.34-alpha — Docker 订阅中心密码引导修复

- 订阅中心首次登录引导区分 Docker 与原生部署：Docker 版在容器内 MiniProbe 管理菜单 15 → 6 设置独立密码。
- 修正网页其它错误地将本地管理描述为原生 SSH 专用的旧提示，并调整主菜单名称。
- Docker CI 新增实际执行菜单 15 → 6 的烟测：密码哈希存储、subscriptions.json 文件权限 0600，以及容器强制重建后的持久化。
- 新增 Go 存储回归与 JavaScript 首次设置提示回归。保留 HTTPS 私有只读网页、本地 Unix Socket 管理和 Docker Socket 隔离。
- Server/UI 版本统一更新为 v0.4.34-alpha；不修改 Agent 接入协议、现有节点与订阅数据、Tunnel Token 和 HomeSphere。

发布提示：仅当本版本 PR 合并到 main，且 GHCR 多架构镜像发布工作流成功后，才可在生产 VPS 通过 MiniProbe Docker 管理菜单 13 检查更新。
