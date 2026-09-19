# MiniProbe v0.4.8-alpha

本版本只收敛两项已经确认的优化，不扩展其它功能。

## 1. 网络标签只保留 V4 / V6

- Dashboard 只显示 `V4`、`V6`。
- 删除 `V4 NAT / IDC / 家宽 / 移动 / 原生 / 广播` 等推断标签。
- 私网 / CGNAT IPv4 也统一显示为 `V4`，只表达“具备 IPv4 地址族”。
- Agent 不再为了标签查询第三方 IP intelligence、GeoIP 或 RDAP 服务。
- 网络接口变化后会低频刷新 V4 / V6 状态。
- Dashboard 同时对旧 Agent 上报的扩展标签做过滤，避免历史标签继续显示。

## 2. IPv6-only VPS 线路测试

- 修复 IPv6-only VPS 使用 IPv4 探测目标导致超时 / N/A 的问题。
- IPv4-only / 双栈节点继续沿用北京、上海、广州的 IPv4 三网目标。
- IPv6-only 节点自动切换到运营商级 IPv6 DNS 目标：
  - 电信：`240e:4c:4008::1`
  - 联通：`2408:8888::8`
  - 移动：`2409:8088::a`
- 新增原生 ICMPv6 Echo Request / Reply 支持；TCP / UDP 继续通过 Go 网络栈直接支持 IPv6。
- IPv6 目标不冒充城市精度，Dashboard 显示 `IPv6电信 / IPv6联通 / IPv6移动`。
- 双栈节点仍优先使用原有 IPv4 城市目标，保证历史延迟基准不被无故改变。

## 兼容性

- 不修改数据库结构。
- 不影响已有节点 Token、流量累计、月租、到期时间、Cloudflare Tunnel、Dashboard 登录或 Telegram 配置。
- Agent 版本更新为 `0.4.8-alpha`。
- Server 安装器默认 Release 更新为 `v0.4.8-alpha`。
