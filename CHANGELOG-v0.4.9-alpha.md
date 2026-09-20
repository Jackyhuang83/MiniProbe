# MiniProbe v0.4.9-alpha

这是一个基于真实 VPS 故障与 IPv6-only 实测结果的可靠性修复版本，并补齐 Agent 集中升级能力，减少多节点日常维护成本。

## IPv6-only 三网线路测试

- 修正 v0.4.8-alpha 把“IPv6 DNS 地址可达”与“该地址一定响应 ICMP Echo”混为一谈的问题。
- 实测确认三运营商 IPv6 DNS 均可正常响应 DNS 查询，但电信、联通目标可能不响应 Ping。
- IPv6-only 节点现在固定使用：
  - 电信：`240e:4c:4008::1`
  - 联通：`2408:8888::8`
  - 移动：`2409:8088::a`
- IPv6-only 线路测量强制使用 UDP/53 DNS 请求 RTT，不再依赖 ICMP Echo，因此不会把“禁 Ping”误判为线路超时。
- Dashboard 对 IPv6-only 节点显示实际协议 `UDP`；线路名仍为 `IPv6电信 / IPv6联通 / IPv6移动`，不伪装成某个城市的固定节点。
- IPv4-only 与双栈节点行为保持不变，继续使用所选北京 / 上海 / 广州目标与 ICMP / TCP / UDP 设置。

## Server 本机 Agent 自恢复

- 修复 Server VPS 自身 Agent 在 Server/公网/DNS/Cloudflare Tunnel 一起中断后，公网已恢复但 Agent 仍可能持续出现 DNS `i/o timeout`、Dashboard 长时间保持离线的问题。
- Agent 检测到同机 MiniProbe Server（`/run/miniprobe/admin.sock` + 本机 `/healthz`）后，固定使用 `http://127.0.0.1:28888` 上报。
- Server 本机 Agent 不再绕行 `公网域名 -> Cloudflare -> Tunnel -> 本机 Server`。
- 一旦确认运行在 Server 主机，本机上报不再回退公网 Endpoint，避免 DNS/Cloudflare Tunnel 故障再次影响 Server 自身节点恢复。
- 上报发生网络或 DNS 失败后，Agent 会重建 HTTP Transport，减少网络恢复后残留连接/解析状态影响。

## Agent 版本管理 / 集中升级

- `miniprobe` 新增 `12. Agent 版本管理 / 集中升级`。
- Server 升级后，可一次为所有支持该能力且版本落后的 Agent 排队升级，不再逐台复制安装命令。
- Agent 只接受 Server Ed25519 签名的专用升级策略；升级策略只包含目标版本、当前架构对应的固定 Agent 资产、SHA256 和文件大小，不包含 Shell 命令，也不能指定任意下载 URL。
- Agent 从当前已连接的 MiniProbe Server `/downloads/` 下载对应架构二进制，校验签名、架构、大小、SHA256，并执行 `--version` 自检后才原子激活。
- 自更新后的二进制保存在 Agent 状态目录的 `bin/miniprobe-agent`，`run-agent.sh` 在服务重启/系统重启后会优先启动该已验证版本；不会覆盖 Token 配置文件。
- 手动执行 Agent 安装/更新命令仍然具有最高优先级，会清除自更新 overlay 并使用刚安装的基础二进制。
- 旧 Agent 不具备自更新代码，因此从 v0.4.8-alpha 或更早版本升级到 v0.4.9-alpha 时仍需要最后一次逐台手动更新；完成这次 bootstrap 后，后续版本即可在 Server 上集中升级。
- 离线但已支持集中升级的节点可以提前排队，重新上线后自动领取升级策略。
- 保持 MiniProbe 的“无任意远程命令”原则：Server 只能触发内置 Agent 软件更新动作，不能借此执行任意命令。

## 保持不变

- 网络标签仍只显示 `V4` / `V6`。
- 月租、到期时间、长期节点和局部修改逻辑保持不变。
- Cloudflare Tunnel、安全模型、流量累计、流量保护、Telegram、15 节点软上限与 2 GiB 存储预算保持不变。

## 版本

- Agent 版本更新为 `0.4.9-alpha`。
- Server 安装器默认 Release 更新为 `v0.4.9-alpha`。
