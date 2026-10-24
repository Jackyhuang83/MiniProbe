# MiniProbe v0.4.39-alpha — 双栈节点可选 IPv6 三网线路测试

- 同时具备 IPv4/IPv6 出站的 VPS 可在 SSH `miniprobe → 3. 修改节点 / 流量策略 → 线路测试 IP 版本` 选择「仅 IPv6」；仅影响选定节点。默认「自动」继续沿用原行为。
- 「仅 IPv6」复用 IPv6-only 的三运营商测试：通过 AAAA 解析与 `tcp6/TCP 80` 测量 TCP 连接往返延迟，Dashboard 显示 `IPv6电信 / IPv6联通 / IPv6移动`、失败率及长期趋势，并使用 ICMPv6 traceroute 为线路详情提供 ASN 路由信息。
- IPv6 不可用时显示探测失败，不会静默回落 IPv4，也不关闭 VPS 的 IPv4 出站；其他节点保持现有 IPv4 城市测试和协议配置。
- 每节点选择由 Server 通过现有 Ed25519 签名 ProbePolicy 下发，并校验 IP 版本字段；网页仍然只读，无 Web 管理写入、Shell 或容器额外权限。
- 包含双栈默认、仅 IPv6 强制探测、签名校验和节点更新的回归测试；保留原有流量统计、订阅管理、Docker/Cloudflare Tunnel 等功能。
- 本次 **Server 与目标 Agent 都需要升级为 v0.4.39-alpha**，旧 Agent 无法识别新的强制 IPv6 策略。升级后在 Server 的 SSH 菜单 12 选择目标节点更新 Agent，再在菜单 3 为需要的 VPS 选择仅 IPv6；不要在确认 Agent 更新前切换探测版本。
- 三运营商官网可能经过 CDN/WAF，此处结果为选定 IPv6 端点的 TCP 连接 RTT 与 ASN 路由参考，**不代表特定城市的完整回程延时**。
- 发布只更新 GitHub Release 二进制与 GHCR 镜像；不会自动替换生产 VPS 容器。按现有 SSH 宿主机菜单 13 更新 Server，确认健康后再按需升级 Agent。
