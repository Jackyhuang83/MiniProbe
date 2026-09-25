# MiniProbe v0.4.11-alpha

## Fix

- 修复 IPv6-only 三网线路延迟长期显示约 0-2 ms、无法代表真实运营商路径的问题。
- IPv6-only 不再使用电信/联通/移动公共 DNS 的 UDP/53 DNS query RTT 作为线路基准。

## Change

- IPv6-only 三网端点改为运营商官方站点：
  - 中国电信：`www.189.cn`
  - 中国联通：`www.chinaunicom.com.cn`
  - 中国移动：`www.10086.cn`
- Agent 对上述域名解析 AAAA 后强制使用 `tcp6` 连接 TCP/80。
- RTT 只统计 TCP connect 时间；DNS 解析时间不计入延迟。
- 探测不发送 HTTP 请求、不执行 TLS 握手，因此不受 HTTP 301/302/403/412、证书或 legacy TLS renegotiation 行为影响。
- IPv6-only Dashboard 行名继续显示 `IPv6电信 / IPv6联通 / IPv6移动`，协议标签改为 `TCP`。
- 每轮 4 次尝试、最近 20 轮失败率窗口保持不变。

## Compatibility

- IPv4-only / 双栈节点继续沿用北京 / 上海 / 广州 IPv4 目标以及用户选择的 ICMP / TCP / UDP 协议，不改变既有基准。
- V4 / V6 地址族判断、NAT VPS、Cloudflare Tunnel、流量计数和集中升级机制保持不变。

## Upgrade notes

- 本版本修改 Agent 探测逻辑，因此 Agent 需要升级到 `v0.4.11-alpha`。
- 已运行 `v0.4.9-alpha` 或更高版本 Agent 的节点可通过菜单 `12. Agent 版本管理 / 集中升级` 批量升级。
