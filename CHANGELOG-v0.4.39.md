# MiniProbe v0.4.39 — 正式稳定版

本版在 `v0.4.39-alpha` 基础上完成实机验证后发布；此前的 `alpha` 版本保留用于回溯。本次正式版不改变已验证的采集逻辑。

## 本版重点

- IPv4 + IPv6 双栈 VPS 可通过 Server SSH `miniprobe → 3. 修改节点 / 流量策略`，在选定节点「名称」输入框直接回车后，将「线路测试 IP 版本」设置为「2. 仅 IPv6」。
- 复用 IPv6-only 节点的电信、联通、移动 AAAA + TCP6/TCP 80 延迟探测和 IPv6 ASN 路由详情；实机已验证网页三网显示正常。
- 「自动」模式保持原有双栈 IPv4 探测行为；修改仅针对所选节点，不更改系统的 IPv4 出站、不干扰 Snell、SS2022、Xray 或其他节点。
- 探测偏好由 Server 的 Ed25519 签名配置发送给 Agent；无效值拒绝，配置 IPv6 后不回退 IPv4。Dashboard 保持只读。
- 继续保留现有订阅中心、Cloudflare Access / Tunnel、流量统计和 Docker 隔离能力；不自动升级生产环境。

## 安装与兼容性

- **Server 与需要使用此功能的 Agent 均须为 `v0.4.39`**。先通过 Server 宿主机 `miniprobe → 13` 更新 Docker Server，再通过 `12` 更新目标 Agent，最后通过 `3` 修改目标节点。
- 网页显示的 IPv6 三网延迟是运营商站点 TCP 连接 RTT，不代表特定城市的精确回程延迟；ASN 路由用于路径观察。
- SHA256SUMS 与 Linux amd64/arm64 Server、amd64/arm64/armv7 Agent 二进制一起由 Release 构建生成。Docker Server 另由 GHCR Actions 构建发布。

## 未来转为私有仓库时的提醒

此 Release 发布时项目仍为公开仓库。将源码仓库改为 Private 后，旧 VPS 上无需立刻停机或重装；但匿名 GitHub Releases / raw.githubusercontent.com 下载以及版本检查可能失效。GHCR 镜像是否仍可匿名拉取取决于对应 Package 的**独立可见性**，必须分别确认。建议先升级和备份，再规划后续私有环境的软件分发与鉴权；切勿在脚本、镜像、日志或仓库内嵌入 GitHub Token。
