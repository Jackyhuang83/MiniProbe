# MiniProbe v0.4.10-alpha

## Bugfix：IPv6-only 节点误标 V4 / 三网误走 IPv4

本版本只修复地址族可用性判断，不增加新的产品功能。

### 修复内容

- 修复 IPv6-only VPS 仅绑定内部 IPv4 地址（例如 `10.10.10.10/32`）但没有 IPv4 出站路由时，Dashboard 仍错误显示 `V4` 的问题。
- 修复同一误判导致 IPv6-only 节点继续选择 IPv4 北京 / 上海 / 广州三网目标、最终全部显示 `timeout` 的问题。
- `V4` / `V6` 不再仅根据网卡是否存在某类地址判断，而改为使用内核实际的 IPv4 / IPv6 出站路由与源地址选择能力。
- Dashboard 网络标签与三网探测地址族选择共用同一套判断逻辑，避免两处结果不一致。
- NAT / CGNAT 节点只要具备可用 IPv4 出站路由，仍正确显示 `V4`；仅存在私网 IPv4 地址而没有 IPv4 出站路由时不显示 `V4`。
- IPv6-only 节点继续使用三运营商 IPv6 DNS 目标，并固定以 UDP/53 DNS 请求 RTT 测量；不回退到 IPv4 城市目标。

### 升级方式

- Server 先升级到 `v0.4.10-alpha`。
- 已运行 `v0.4.9-alpha` 的 Agent 无需再逐台 SSH；可在 `miniprobe` → `12. Agent 版本管理 / 集中升级` 中批量升级到 `v0.4.10-alpha`。
- 集中升级仍使用 Ed25519 签名的受限软件更新策略，不提供任意远程命令。

### 兼容性

- Server：amd64 / arm64。
- Agent：amd64 / arm64 / armv7。
- systemd / OpenRC 安装方式保持不变。
- 数据库、节点 Token、流量累计状态和现有 Dashboard / Telegram 配置不需要迁移。
