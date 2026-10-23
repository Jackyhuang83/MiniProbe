# MiniProbe

MiniProbe 是一个面向个人 VPS 集群的轻量监控探针，重点支持普通 VPS、NAT VPS、IPv6-only VPS，并把线路延迟、丢包和流量安全放在第一优先级。

当前发布目标：`v0.4.30-alpha`（订阅管理中心 DEV1–DEV3，Server-only）；上一个已发布版本：`v0.4.29-alpha`

GitHub：`https://github.com/Jackyhuang83/MiniProbe`

> 设计规模：正常使用 `≤ 15` 个节点。第 16 个节点开始只告警、不强制禁止。
>
> 存储目标：MiniProbe 自身持久化数据有明确上限，不把 1 秒遥测永久写盘；长期运行也不能因为“运行时间越久”无限增长。硬预算为 `2 GiB`。

## 设计原则

- Direct `http://IP:28888` 仅作为最简单的首次部署 / 故障恢复入口；它没有 TLS，不建议长期暴露在公网。
- 正式公网使用推荐 Cloudflare Tunnel / HTTPS；验证成功后 MiniProbe 仅监听 `127.0.0.1:28888`，公网 `IP:28888` 不再提供访问。
- Web 只负责查看 Dashboard；节点管理只允许 SSH 登录 Server 后执行 `miniprobe`。
- Agent 主动连接 Server，不需要 Agent 开放任何 MiniProbe 入站端口。
- 无 WebShell、无 SSH 管理、无任意远程命令执行。
- 流量保护关机是 Agent 内置的窄权限保护动作，不提供通用 Shell 执行接口。
- Server 下发给 Agent 的配置使用 Ed25519 签名，Agent 拒绝无效签名和旧版本重放策略。
- 兼容 systemd / OpenRC，目标发行版包括 Debian、Ubuntu、Alpine、Rocky Linux、AlmaLinux、CentOS Stream。

---

# 1. 首次部署 / 故障恢复：Direct HTTP

发布 `v0.4.16` GitHub Release 后，Server 端默认从该 Release 下载二进制，只需要一条安装命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Jackyhuang83/MiniProbe/main/scripts/install-server.sh | bash
```

默认：

```text
http://SERVER_IP:28888
```

首次安装默认：

```text
Dashboard = Password Protected
```

安装器会显示一次随机 Dashboard 密码。

然后 SSH 登录 Server：

```bash
miniprobe
```

菜单示例：

```text
============================================================
                     MiniProbe
============================================================
 Agent 接入地址 : http://SERVER_IP:28888
 网络模式        : Direct IP:28888
 Dashboard       : Password Protected
 节点            : 5 / 15（建议上限）
------------------------------------------------------------
 1. 添加监控节点
 2. 查看节点
 3. 修改节点 / 流量策略
 4. 获取 Agent 安装命令
 5. 删除节点
 6. 重置节点 Token
 7. Dashboard 设置
 8. 安全访问（HTTPS / Cloudflare Tunnel）
 9. Telegram 通知
10. 存储 / 当前设置
11. 国内线路测试（北京 / 上海 / 广州）
12. Agent 版本管理 / 集中升级
 0. 退出
------------------------------------------------------------
```

## 添加 Agent

选择：

```text
1. 添加监控节点
```

最少只需要输入节点名称；其它字段都可按需设置。

例如某个 200 GB 出网流量套餐节点，可以设置：

```text
节点名称: Aliyun-HK
月流量 GB: 200
流量计费方向: 出网
每月流量重置日: 1
计费时区: +08:00
达到 95% 自动保护关机: Y
```

创建后 MiniProbe 自动生成 Agent 一键安装命令。复制到目标 VPS 执行即可。

流量配额中的 `GB` 使用十进制定义：`1 GB = 1,000,000,000 bytes`。这是刻意的安全设计，避免把云厂商标注的 200 GB 错按 200 GiB 计算，从而把保护阈值放大。

Agent 安装器会自动识别：

- linux/amd64
- linux/arm64
- linux/armv7
- systemd
- OpenRC

Agent 不需要开放任何 MiniProbe 入站端口，因此适合 NAT VPS。

---

# 2. IPv6-only

MiniProbe 的 Agent 安装脚本和 Agent 二进制由 MiniProbe Server 自己提供。

因此 IPv6-only VPS 不需要为了安装 Agent 再访问 IPv4-only 下载站。

只要 Agent 接入地址本身可以通过 IPv6 访问即可，例如：

```text
http://[2001:db8::10]:28888
```

或者更推荐双栈 / Cloudflare 域名：

```text
https://probe.example.com
```

IPv6-only Agent 会自动改用三大运营商官方站点作为 IPv6 线路端点：电信 `www.189.cn`、联通 `www.chinaunicom.com.cn`、移动 `www.10086.cn`。Agent 解析 AAAA 后强制使用 `tcp6` 连接 TCP/80，仅统计 TCP connect RTT，不发送 HTTP 请求、不进行 TLS 握手。Dashboard 行名显示 `IPv6电信 / IPv6联通 / IPv6移动`，协议显示 `TCP`。双栈节点仍沿用所选城市的 IPv4 基准。

---

# 3. Cloudflare Tunnel

MiniProbe 允许先用 Direct HTTP 完成首次部署，但 Direct 模式没有 TLS。正式公网使用推荐切换 Cloudflare Tunnel / HTTPS。

确认 Direct 模式工作正常后，可以运行：

```bash
miniprobe
```

选择：

```text
8. 安全访问（HTTPS / Cloudflare Tunnel）
2. Cloudflare Tunnel HTTPS
```

MiniProbe 会提示你先在 Cloudflare 创建一个 **Remotely-managed Tunnel**，并将 Public Hostname 的 Service 指向：

```text
http://127.0.0.1:28888
```

然后输入：

```text
Tunnel 域名
Tunnel Token
```

MiniProbe 会自动执行：

```text
安装 cloudflared
      ↓
启动独立的 miniprobe-cloudflared 服务
      ↓
验证 https://你的域名/healthz
      ↓
通过签名配置让在线 Agent 自动迁移到 HTTPS Tunnel
      ↓
再次验证 Agent 已经迁移
      ↓
MiniProbe 改为只监听 127.0.0.1:28888
      ↓
公网 IP:28888 停止提供访问
```

如果 Tunnel 验证失败，MiniProbe 不会关闭 Direct 端口。

如果有离线节点或过旧 Agent，MiniProbe 会拒绝自动切换，避免节点被锁在旧地址。

切回 Direct 模式时流程相反：先恢复公网监听，再迁移 Agent，最后停止 Tunnel。

Cloudflare Tunnel Token 只保存在 Server 本机 `/etc/miniprobe/cloudflared.env`，权限 `0600`。

---

# 4. Dashboard

Web Dashboard 保持只读。

SSH 菜单中可选择：

```text
1. Public
2. Password Protected
3. Disabled
```

### Public

任何能访问 MiniProbe 的人都可以查看监控数据，但没有管理能力。

### Password Protected

只读 Dashboard 使用独立密码：

- 不保存明文密码
- 随机 Salt
- PBKDF2-HMAC-SHA256
- 登录速率限制
- HttpOnly / SameSite Session Cookie
- 登录成功后当前浏览器 / 设备信任 30 天；30 天到期后重新输入密码
- Session 使用 Server 本地 Secret 做 HMAC 签名，不在磁盘保存明文密码或原始 Session Token
- 修改 Dashboard 密码会立即使旧的 30 天可信会话失效
- HTTPS / Cloudflare Tunnel 下 Cookie 自动启用 `Secure`

如果 Direct 模式仍使用 HTTP，Dashboard 会显示未加密警告。密码和会话传输本身没有 TLS 保护；正式公网使用应切换 Cloudflare Tunnel / HTTPS。

### Disabled

关闭：

```text
/
/api/v1/nodes
```

但 Agent 所需接口继续工作：

```text
POST /api/v1/report
GET  /install-agent.sh
GET  /downloads/...
GET  /healthz
```

---

# 5. 监控指标

Agent 当前采集：

- CPU 使用率、核心数、CPU 型号
- RAM / Swap
- Load 1 / 5 / 15
- 磁盘使用量
- 实时入网 / 出网速度
- 本计费周期入网流量
- 本计费周期出网流量
- 已用计费流量 / 总流量
- 流量使用比例
- TCP / UDP 连接数
- 进程数
- uptime
- OS / Kernel / Architecture / Virtualization
- IPv4 / IPv6
- V4 / V6 地址族标签（只显示本机可直接确认的事实，不再推断 NAT / IDC / 家宽 / 原生 / 广播）
- 国内三网线路延迟 / 失败率（Server 全局选择北京、上海或广州）
- 探测协议可选 ICMP / TCP / UDP
- ICMP：Ping RTT + packet loss
- TCP：IPv4 城市目标使用 TCP/53 connect RTT；IPv6-only 使用运营商官网 AAAA + tcp6/TCP 80 connect RTT
- UDP：UDP/53 DNS query RTT + query failure rate
- Dashboard 分开显示最近 20 次延迟历史与失败/丢包历史（探测约每 10 秒更新一次）
- 右侧失败率 / 丢包率使用与这 20 个历史块一致的滚动窗口；每轮 4 次探测，因此完整窗口最多统计 80 次尝试

Dashboard 节点卡片直接显示月流量，而不是拿“开机以来网卡总流量”冒充月流量。

## 国内线路测试

SSH 执行 `miniprobe`，进入 `11. 国内线路测试` 后可全局选择：

```text
城市：北京 / 上海 / 广州
协议：ICMP / TCP / UDP
状态：开启 / 关闭
```

城市和协议由 Server 通过独立的 Ed25519 签名 Probe Policy 下发给所有 Agent。Dashboard 线路名直接显示城市，例如 `广州电信 / 广州联通 / 广州移动`，右上角用轻量标签显示当前协议。

预设目标使用对应城市运营商 DNS 节点；TCP / UDP 使用 53 端口，ICMP 使用 Echo Request。不同协议的“失败率/丢包”语义不同，因此 Dashboard 同时保留协议标签，避免把 TCP/UDP 失败率误读成 ICMP packet loss。

IPv4-only 和双栈 Agent 继续使用所选城市的 IPv4 运营商 DNS 目标。IPv6-only Agent 自动改用运营商官方站点：电信 `www.189.cn`、联通 `www.chinaunicom.com.cn`、移动 `www.10086.cn`。Agent 只解析这些域名的 AAAA，并强制通过 `tcp6` 连接 TCP/80；计时从 DNS 解析完成后开始，因此 Dashboard 记录的是 TCP connect RTT，而不是本地 DNS 查询耗时。网站的 HTTP 301/302/403/412 或 TLS 行为不参与线路判定。由于这些站点可能使用运营商 CDN/WAF，不冒充北京 / 上海 / 广州城市节点，Dashboard 行名仍显示 `IPv6电信 / IPv6联通 / IPv6移动`。双栈节点继续沿用原有城市 IPv4 测试，避免改变既有基准。

丢包 / 失败率的显示口径与右侧 20 个历史块一致，不再只显示“最后一轮 4 个包”的瞬时结果。窗口未填满时按已经实际执行的探测次数计算。ICMP 仍按每轮 4 个 Echo Request 统计；TCP / UDP 则对应 4 次连接 / DNS 查询尝试。

## 线路详情 / 长期趋势

从 `v0.4.12-alpha` 起，主 Dashboard 仍保持紧凑，只在线路区域下方增加“线路状态 / 查看线路详情”。点击后进入独立线路详情页，不把大型折线图塞进节点卡片。

线路历史由 Server 单独保存，不写入主 `miniprobe.json` 高频状态数据库：

```text
最近一天：1 分钟粒度
最近一周：5 分钟聚合
最近一月：30 分钟聚合
原始历史：最多保留 31 天
线路历史硬上限：256 MiB
```

Agent 仍约每 10 秒执行一次三网探测。Server 对同一分钟内的新探测轮次去重并聚合后，只追加一条分钟记录，因此不会把每 2 秒 Agent 上报永久落盘。正常 15 节点规模下，30 天线路数据保持在 MiniProbe 的 2 GiB 总存储预算之内；达到线路历史硬上限时优先删除最旧的日文件。

线路详情同时支持 ASN 路由基准：

```text
Agent 约每 30 分钟执行一次 ICMP traceroute
-> 仅把本次原始 hop 临时上报给自己的 MiniProbe Server
-> Server 对公开 hop 做 IP -> ASN 映射
-> 只持久化 Hop 编号 + ASN 路径
-> 原始 hop IP 不写入主数据库、线路历史或 Dashboard
```

ASN 映射由 Server 使用 Team Cymru 的 DNS IP-to-ASN 社区服务完成，只用于线路路由识别，不用于 GeoIP、节点位置或网络标签判断。ASN 路径第一次成功采集时自动建立基准；出现不同路径时必须连续两次确认才标记“路由变化”，避免单次 traceroute 丢跳造成误报。最近最多保留 50 条“变化确认 / 恢复基准”事件，并在对应运营商的长期 RTT 图上标出事件时间，便于判断路由变化是否伴随延迟变化。

从 `v0.4.13-alpha` 起，线路详情会在 ASN 路径上方给出可读的骨干网识别：电信 `AS4809 -> CN2`、`AS4134 -> 163 / ChinaNet`；联通 `AS9929 -> CUII / 9929`、`AS4837 -> 4837 / China169`；移动 `AS58807 -> CMIN2`、`AS58453 -> CMI`、`AS9808 -> CMNET`。高优先级骨干标识优先于普通骨干，例如同一路径同时出现 `AS4809` 与 `AS4134` 时显示 `CN2`。识别不到关键 ASN 时明确显示“其他 / 未识别”，不根据运营商名称猜测，也不进一步声称 CN2 GIA 等仅凭 ASN 路径无法可靠确认的产品等级。路由事件同时显示“CN2 -> 163 / ChinaNet”一类可读变化，并保留完整 ASN Path 供排查。

从 `v0.4.15-alpha` 起，Dashboard 使用版本化路径 `/ui/<Server版本>/...` 作为静态资源缓存键，不再依赖 Query String。这样即使 Cloudflare 缓存规则忽略 `?v=`，不同 Server/UI 版本也会落到完全不同的 URL。未版本化的 HTML 会在源站重定向到当前 `/ui/<版本>/`；未版本化静态资源返回 `no-store`；版本化 JS/CSS/SVG 可安全长期缓存。Dashboard Session 仍返回 `server_version`，旧 UI 发现 Server 版本变化后会跳到新的版本化路径。

Dashboard 仍然只读。如果确实需要把当前 ASN 路径重新设为基准，只能通过 Server SSH：

```text
miniprobe
-> 11. 国内线路测试
-> 4. 重置节点线路基准（使用当前 ASN 路径）
```

如果 VPS / 容器环境不允许 raw ICMP，常规 RTT/失败率探测仍继续工作；该节点的 ASN 路由区域会显示暂不可用，不会把路由功能失败误判成节点离线。

## IP 地址族标签

Dashboard 的网络标签只保留 `V4` 与 `V6`。Agent 直接从本机网络接口判断地址族，不再调用第三方 IP intelligence / GeoIP / RDAP 服务，也不再显示 `V4 NAT / IDC / 家宽 / 移动 / 原生 / 广播` 等容易误判的属性。

私网或 CGNAT IPv4 接口统一显示为 `V4`；存在可用全局 IPv6 地址时显示 `V6`。双栈节点显示 `V4 · V6`。

---

# 6. 月流量计费与保护关机

每个节点都可以独立设置：

```text
流量总额
计费方向
重置日
计费时区
是否启用保护关机
保护阈值
```

计费方向：

```text
outbound  = 只计算出网
inbound   = 只计算入网
total     = 入网 + 出网
```

Agent 使用自己的持久化计数器：

```text
/var/lib/miniprobe-agent/state.json
```

因此：

- Agent 重启不会归零
- VPS 重启不会直接把本周期累计值归零
- 网卡内核 counter 重置时不会做负数回退
- 每月按配置的重置日自动开启新计费周期

> 注意：首次安装 Agent 时，MiniProbe 从安装时刻开始建立本地流量基线，不会凭空知道云厂商在本计费周期更早已经统计的用量。如果是在月中首次启用“保护关机”，应给阈值留足安全余量，或者在下一个重置周期开始后再把 MiniProbe 计数当作完整月度计费参考。云厂商最终账单仍以云厂商控制台为准。

默认流量提醒级别：

```text
70%
85%
90%
```

保护关机默认阈值：

```text
95%
```

保护关机默认由用户在节点配置时主动开启。

Agent 没有任意命令执行接口。保护关机只能触发预先内置的关机动作。

---

# 7. Telegram

SSH 菜单：

```text
9. Telegram 通知
```

支持：

## 节点掉线告警

默认连续离线：

```text
2 分钟
```

才发送 Telegram，避免短暂网络抖动刷屏。

恢复上线后发送恢复通知。

## 流量阈值告警

默认在：

```text
70%
85%
90%
```

发送分级流量告警。

如果 Agent 触发保护关机，Server 会发送保护告警。

## 定时流量摘要

默认可选择：

```text
每 4 小时
每 8 小时
每 12 小时
每天一次
关闭
```

推荐：

```text
每 4 小时
```

15 个节点会合并成一条 Telegram 摘要，不会每个节点单独刷一条消息。

摘要包含：

```text
入网流量
出网流量
已用计费流量 / 总流量
使用比例
最近一个摘要周期新增入网 / 出网
流量重置日 / 时区
```

---

# 8. 节点数量

MiniProbe 是个人项目，不按商用监控平台设计。

推荐规模：

```text
≤ 15 个节点
```

第 16 个节点开始：

- 仍然允许添加
- SSH 菜单明显提示
- Dashboard 顶部明显提示
- 如果 Telegram 已启用，在首次跨过 15 → 16 时发送提醒

不会硬性锁死用户。

---

# 9. 存储上限

MiniProbe 不把 1 秒实时遥测永久写入数据库。

当前持久化内容主要是：

- 节点配置
- 节点最新状态
- 小型线路状态条
- Telegram 告警状态
- Server 配置

因此数据量不会因为运行 3 个月 / 3 年而按秒线性增长。

硬设计预算：

```text
MiniProbe 持久化总预算：2 GiB
```

数据库文件本身还有更低的安全限制；当前版本不会保存无限历史数据。

SSH 菜单：

```text
10. 存储 / 当前设置
```

可以直接查看当前占用和 2 GiB 上限。

日志策略：

- 正常 Agent 上报不逐条写日志
- Agent 连续连接失败最多约每分钟打印一次
- systemd 服务启用日志速率限制
- OpenRC 默认不写无限增长的 MiniProbe 文件日志

原则：

> 宁可不保存高频历史，也不能让探针把 5 GB 小硬盘跑满。

---

# 10. 安全边界

公网 TCP 服务只包含：

```text
Agent 上报
Agent 安装脚本 / 二进制
healthz
可选只读 Dashboard
```

不存在公网管理 API：

```text
添加节点
删除节点
Token 重置
Server 配置修改
WebShell
SSH
任意远程命令执行
远程文件管理
```

管理路径固定：

```text
SSH
  ↓
Server VPS
  ↓
miniprobe
  ↓
/run/miniprobe/admin.sock
```

Unix Socket 默认权限：

```text
0600
```

Agent 配置同步：

```text
Server Ed25519 签名
        ↓
Agent 验证签名
        ↓
拒绝伪造 / 旧版本重放策略
```

配置同步只包含固定字段，例如 Server Endpoint 和流量策略，不存在 Shell 命令字段。

---

# 11. 本地 Bundle 测试

源码包中编译 Release 后：

```bash
make test
make release
```

本机 Linux Server 测试安装：

```bash
sudo MINIPROBE_BUNDLE_DIR="$PWD/dist" sh scripts/install-server.sh
```

随后：

```bash
miniprobe
```

Release 产物：

```text
miniprobe-server-linux-amd64
miniprobe-server-linux-arm64
miniprobe-agent-linux-amd64
miniprobe-agent-linux-arm64
miniprobe-agent-linux-armv7
SHA256SUMS
```

---

# 12. Agent 集中升级

从 `v0.4.9-alpha` 开始，Agent 支持受限的软件自更新。SSH 登录 Server 后运行：

```bash
miniprobe
```

进入：

```text
12. Agent 版本管理 / 集中升级
```

可查看每个节点的 Agent 版本和升级状态，并选择“一键升级全部可升级 Agent”或只升级单个节点。Server 只会为版本落后的、已声明 `self-update-v1` 能力的 Agent 下发升级。离线节点也可以排队，重新上线后领取。

安全边界保持不变：集中升级不是远程 Shell。Server 下发的是 Ed25519 签名的固定 `UpgradePolicy`，其中只允许当前节点、当前目标版本、当前 CPU 架构对应的 MiniProbe Agent 文件、SHA256 和大小。Agent 不接受任意命令，不接受任意 URL，也不会写入 `agent.env`。下载完成后还会运行新二进制的 `--version` 自检，全部通过才切换。

从 `v0.4.13-alpha` 起，Server 版本与“目标 Agent 版本”显式解耦。纯 Server / Dashboard 更新不会为了追求版本号一致而强制所有 Agent 重装；菜单 12 会单独显示 Server 版本和当前目标 Agent 版本。

需要注意一次性 bootstrap：`v0.4.8-alpha` 及更早 Agent 本身没有自更新代码，因此升级到 `v0.4.9-alpha` 时仍需最后一次逐台执行安装/更新命令。所有节点进入 `v0.4.9-alpha` 后，后续 MiniProbe 版本即可从 Server 统一升级。

---

# 13. 当前版本说明

`v0.4.16` 是经实机验证后的正式稳定版：保留 v0.4.16-alpha 已验证的版本化 UI 根路径重定向修复，继续使用 `/ui/<Server版本>/...` 作为缓存隔离路径，并保留 v0.4.13-alpha 的可读骨干网识别。Agent 探测逻辑仍保持 v0.4.12-alpha，无需为了正式版标签重复升级 Agent：

```text
Direct HTTP 用于首次部署 / 故障恢复
Cloudflare Tunnel HTTPS 作为推荐正式公网入口
NAT / IPv6-only Agent
只读 Dashboard
本机 SSH 管理菜单
Telegram 掉线 / 恢复 / 流量提醒
每 4 小时流量摘要
持久化月流量计数
95% 可选保护关机
15 节点软上限
2 GiB 存储硬预算
签名 Agent 配置同步
Dashboard 不显示节点公网 IP
节点名前按名称/标签识别常见国家或地区旗帜
系统信息显示 Debian / Ubuntu / Alpine 等识别图标
系统信息行只显示 V4 / V6，不推断 NAT / IDC / 家宽 / 移动 / 原生 / 广播
V4 / V6 按内核实际可用出站路由判断；仅存在 10.x / 172.16-31.x / 192.168.x 等内部 IPv4 地址但没有 IPv4 出站路由时，不再误标为 V4
三网探测与 Dashboard V4 / V6 标签共用同一套地址族可用性判断，避免 IPv6-only 节点误走 IPv4 三网目标
Agent 不为网络标签调用第三方 IP intelligence / GeoIP / RDAP
IPv6-only 节点改用三运营商官方站点域名；解析 AAAA 后强制 tcp6/TCP 80，只测 TCP connect RTT，避免运营商 DNS Anycast/近端解析节点导致 0-2 ms 的失真结果
主 Dashboard 保持紧凑，只新增线路状态与“查看线路详情”入口
线路详情页提供最近一天 / 一周 / 一月 RTT 与失败率曲线
Server 以分钟聚合追加线路历史，原始历史保留 31 天并设置 256 MiB 独立硬上限
Agent 约每 30 分钟执行一次轻量 ICMP traceroute，Server 集中做 ASN 映射
ASN 路由变化需连续两次确认；最多保留 50 条变化/恢复事件，并在 RTT 曲线上标记
线路详情将关键 ASN 翻译为 CN2 / 163、CUII / 9929 / 4837、CMIN2 / CMI / CMNET，并在路由事件中显示可读线路变化
原始 traceroute hop IP 只在 Agent -> Server 处理过程中短暂存在，不写入主数据库、线路历史或 Dashboard
线路基准只能通过 SSH 菜单 11 重置，Dashboard 继续保持只读
IPv4-only / 双栈节点继续沿用原北京 / 上海 / 广州三网测试与用户选择的 ICMP / TCP / UDP 协议
Server 本机 Agent 检测到本机 MiniProbe Server 后固定走 http://127.0.0.1:28888，上报不再依赖公网 DNS / Cloudflare Tunnel 回环
一旦确认运行在 Server 主机，本机 Agent 不再回退公网 Endpoint，避免 DNS / Tunnel 故障再次影响自恢复
Agent 上报网络/DNS失败后重建 HTTP Transport，增强断网恢复能力
SSH 菜单新增 Agent 版本管理 / 集中升级；支持的 Agent 可在 Server 上一次性排队升级
集中升级只下发 Ed25519 签名的固定软件更新策略，不提供任意 Shell/远程命令
Agent 下载 Server 当前 Release 对应架构二进制，校验架构、大小、SHA256 与内置版本后再切换
旧版本 Agent 首次升级到 v0.4.9-alpha 仍需手动一次；之后版本可集中升级
月租 / 到期剩余时间进入节点卡片；到期日输入 L/长期可标记为长期续费，Dashboard 显示“长期”
节点资料支持局部修改：回车保持原值，只保存实际填写项目
线路质量拆成延迟历史 + 丢包/失败历史两组；右侧百分比按同一 20 轮滚动窗口计算
线路区域右上角显示实际使用协议；IPv6-only 显示 TCP
Dashboard 正确登录后信任当前设备 30 天
Direct HTTP 明确标注为未加密，Dashboard 显示安全警告
Server 安装器升级时会显式重启正在运行的 systemd 服务
```

长期续费节点在 SSH 菜单添加/修改到期日时，可直接输入：

```text
L
```

或：

```text
长期
```

MiniProbe 内部使用 `2036-01-01` 作为兼容标记，Dashboard 显示为 `长期`，而不是显示一个很大的“剩余天数”。普通节点仍使用 `YYYY-MM-DD`。

目前不追求商用规模，也不加入远程服务器管理功能。


## v0.4.17-alpha（待发布）：防止本机 Server 误识别

修复 Agent 在被监控 VPS 同时运行了另一套 MiniProbe Server 时，仅凭 `admin.sock` 和 `/healthz` 成功便错误选择 `127.0.0.1:28888`，导致持续 HTTP 401 离线的问题。

现在只有本机 Server **实际接受当前 Agent 的 Bearer Token** 时，才允许通过 loopback 上报；否则继续使用签名策略或安装配置给出的正确接入地址。身份验证只发送不完整 JSON，不写入正常报告、不修改节点或流量计数。

此前已添加的 `/etc/systemd/system/miniprobe-agent.service.d/force-remote.conf` 临时绕过配置，可在部署新 Agent 且验证正常后移除。

## v0.4.18-alpha（开发版，待验证）

- 新增出口 IPv4/IPv6 的 ASN 编号及路由公司名称：Agent 每 12 小时通过固定 HTTPS IP 回显端点发现出口地址，Server 复用 Team Cymru DNS 查询 ASN 及公司名称。公网 IP 仅在认证上报中暂时传输；不写入数据库、不暴露给 Dashboard。
- 新增平台状态：ChatGPT、Gemini、Claude、Google 搜索、Netflix、Disney+、YouTube Premium、TikTok、Reddit。后台低频探测、最多 3 个检测并发、单请求超时 8 秒、响应体最多 256 KiB；不下载视频或测速，不写逐次日志。
- **结果不夸大**：仅验证页面/地区响应的检测显示「可访问」，不等于已登录、流媒体可播放或完整解锁；明确限制显示「地区不可用」，无法判定显示「未知」，Netflix 两个标题均 404 显示「受限」。
- IP 属性保持此前经过验证的 V4/V6 出站路由标签，不根据 ASN 名称猜测「家宽、机房、原生、广播」。
- 旧 Agent 可以继续上报资源和流量；新检测信息需将 Agent 更新至 v0.4.18-alpha。

## v0.4.19-alpha：升级后旧 Dashboard 链接不再 404

- 修复使用旧版收藏夹、历史记录、长期打开的标签页访问 `/ui/旧版本/` 发生 404 的问题。
- 旧版 `/ui/<版本>/`、`index.html`、`network.html?id=...` 及单层 `js/css/svg` 资源使用 HTTP 307 跳转到当前 Server UI 版本。保留 `id` 等查询参数，删除过时缓存参数 `v`，并设置 `Cache-Control: no-store` 防止旧跳转长时间缓存。
- 新版首页与资源仍使用独立版本路径缓存；不改变现有 Dashboard 登录、API 访问控制或已有资源计数。
- 这是 Server / Dashboard 修复版，Agent 目标版本维持 `v0.4.18-alpha`，无需集中升级所有 Agent。
- **兼容范围**：修复到达源站的新请求。若上游 Cloudflare 配置单独缓存了旧 404 响应，必须等待其 TTL 到期或清理缓存；本站不再产生旧版 UI 404。

## v0.4.20-alpha：彩色平台检测与 myTV SUPER

- Dashboard 平台状态采用内嵌 SVG：绿色圆勾表示检测到可访问，橙色横杠表示部分受限，红色叉表示明确的地区限制，灰色问号表示信息不足。图标不依赖外部图片或 emoji 字形。
- 状态采用紧凑标签，平台名后仅显示该平台检测到的国家/地区简写（如 `JP`、`HK`）。Gemini 的常见三位国家代码在 UI 中映射为两位代码。**没有可靠地区信号时不补猜国家**。
- Agent 新增 **myTV SUPER** 地区会话检查，最多 10 项平台检测，沿用每 12 小时一次、最多 3 个并发、8 秒单请求超时和响应大小上限。
- myTV SUPER 通过不需要登录的地区会话接口判断：`region=1` 代表 HK 地区可访问，`region=0` 代表地区不可用；接口异常、未知字段或地区无法判定时显示未知。此检测不代表账号、DRM、付费节目或实际视频播放一定可用。
- 原有 9 项平台仍按现有信号判定；不把单纯网站可访问误写成完整解锁。ASN 信息及现有流量统计不变。
- 更新展示 UI 需要 v0.4.20-alpha Server，更新 myTV SUPER 检测需要 v0.4.20-alpha Agent。可先单节点验证后集中升级。

## v0.4.21-alpha：Server 本机更新菜单与安全校验

- SSH 登录主 Server 后运行 `miniprobe`，新增 **13. Server 检查更新 / 升级**。检测官方 GitHub Releases 最新版本、对比当前 Server 版本，并要求明确输入 `YES` 才开始更新。
- 只支持预定义的 MiniProbe 官方 Server / Agent 二进制，不接收任意命令或下载地址；校验 Release SHA256SUMS，先备份旧文件，原子替换，重启 Server，再通过仅本机的管理 Socket 验证**真实运行版本**。
- 若新程序无法成功重启或运行版本不符合预期，尝试回滚旧 Server 和 Agent 文件；失败时明确报错，不谎称成功。新版本安装成功后管理菜单自动退出，用户重新运行 `miniprobe`。
- 同步加强 `scripts/install-server.sh`：所有二进制在替换之前必须通过 SHA256 校验，且安装后需从本机管理 Socket 确认运行版本；验证失败时尝试恢复旧程序并报错。
- **这是 Server 更新功能**，Agent 目标版本继续保持 `0.4.20-alpha`，无需让全部节点为了菜单更新而重装。
- 由于旧版没有菜单 13，**首次升级至 v0.4.21-alpha 仍须执行一次官方安装脚本**；之后的新版本可在菜单 13 内进行 Server 更新。

## v0.4.22-alpha：更多地区简写（区分来源）

- 现有平台检测仅少数站点返回国家码；新增每 12 小时至多一次轻量 `https://www.cloudflare.com/cdn-cgi/trace` 检测 VPS 默认出口地区的 `loc`，与原 10 项平台检测共享 3 并发限额，不引入 GeoIP/IP 属性库。
- 平台或站点请求有直接地区数据时继续显示例如 `JP`、`HK`，并标记 `region_source=site`；仅无数据时显示 **`≈JP`**，标记 `region_source=egress`。`≈` 只是默认出口地区参考，不等于平台实际解锁地区；浮层说明来源。
- 出口地区服务无法访问或返回无效 `loc` 时仍不填写地区；不会推断 Netflix 可播放地区、MyTV SUPER DRM / 付费解锁情况。
- 兼容旧 Agent 未发送 `region_source` 的报告；Server 保持 Dashboard 只读、ASN、月流量累计不变。更新数据来源需升级 Agent 到 `v0.4.22-alpha`。
- 本版本在已发布的 v0.4.21-alpha Server 菜单更新基础上开发，按版本顺序发布。

## v0.4.23-alpha：可选 IP 属性（默认关闭）

- SSH 登录主 Server 后运行 `miniprobe` → **14. IP 属性检测（默认关闭）**。只有在 SSH 菜单明确输入 `YES` 同意传送节点出口 IP 给第三方 `ipquery.io` 才会启动。Dashboard **严格只读**，面板中显示为默认**折叠**的 IP 属性详情。
- 每节点 **IPv4 与 IPv6 分别最多每天一次（UTC 自然日）**，失败也消耗当天一次额度；首次开启需要等待 Agent 下次出口 IP 上报（正常每 12 小时一次），不要求更新 Agent。由 Server 顺序请求，每次最多 8 秒且限制响应长度；关闭后不再调度新的查询。
- 数据源：`https://api.ipquery.io/{IP}`，无需 API Key，查询时会向第三方披露被查询节点的出口公网 IP。仅持久化 `IDC / 移动 / 非 IDC / 未知`、地区码、已知 VPN/代理/Tor 标记和最后查询时间；**不保存第三方查询的原始 IP**，不展示节点原始 IP。
- **非 IDC 不等于已证实家宽**：界面显示「非 IDC · 家宽待核实」。该数据源不提供可信的原生/非原生依据，因此原生属性始终显示「未知」，绝不因 ASN、地区或未命中代理标记就猜测“原生”。第三方结果只是参考，不保证流媒体解锁。
- 本版本为 **Server-only** 更新；Agent 目标版本继续为 `v0.4.22-alpha`，无需为此功能升级全部 Agent。

## v0.4.24-alpha：修复 Server 更新菜单 Release 元数据过大

- 修复 SSH 菜单 **13. Server 检查更新 / 升级** 出现「检查更新失败: 更新元数据超过大小限制」的错误。根因是原先从 GitHub API 一次拉取 20 条 Release，真实响应约 263 KiB，超过此前 128 KiB 的硬上限。
- 现在仅请求最近 **5 条** Release（发布数量足够覆盖正常升级场景），元数据上限调整为 **1 MiB**，并且维持封顶读取、合法版本过滤及草稿过滤；实际样本约 66 KiB。
- 新增 CI 回归测试：解析超过 128 KiB 的有效元数据、排除 draft、拒绝超过 1 MiB 的输入。
- 此版本仅更新 Server，目标 Agent 继续保持 `v0.4.22-alpha`；不会改变节点数据、Cloudflare Tunnel 或 IP 属性模块的默认关闭设置。
- 当前运行 v0.4.22-alpha / v0.4.23-alpha 的 Server 因菜单 13 的检测错误无法自行更新至本补丁，首次需运行官方 `scripts/install-server.sh` 一次，升级至 v0.4.24-alpha 后菜单 13 恢复正常。

## v0.4.25-alpha：流量历史与统计中心（Server-only）

- 新增独立的 **流量统计中心**，主 Dashboard 顶部点击「流量统计」进入；可看每日入站/出站趋势、计费流量、服务器排名、当前计费周期额度与余额，并支持 7 / 30 / 90 天和自定义日期。
- Server 按 **15 分钟检查点** 保存 Agent 现有流量快照，历史最长 **90 天**、模块最多 **64 MiB**，仍遵守全局 **2 GiB** 总存储预算。普通 Agent 上报不触发额外磁盘写入；历史存储繁忙时优先保证核心监控。
- 每日历史根据同一计费周期相邻累计值的差额得出，不把首个快照的历史累计量当作当天流量。对离线长时间缺口、重置、反常计数等标注数据不完整；展示仅供分析，**不替代云服务商账单**。
- 计费周期用量与余额仍采用 Agent 原有计费状态；历史数据不会触发保护关机、通知或新策略。修复单节点历史不可读时的页面降级、已删除节点历史并发重建问题。
- **Server v0.4.25-alpha，目标 Agent 继续是 v0.4.22-alpha，无需集中升级 Agent。** 历史从安装新版 Server 后开始积累，不会倒推升级前的日历史。

## v0.4.26-alpha：流量排名增加使用率与到期提醒（Server-only）

- 「流量统计中心」→「服务器流量排名」末尾增加 **流量使用情况** 和 **订阅到期** 两列，不改变原有按日期区间计费流量增量排序。
- 使用率取当前 Agent 月度计费周期：**低于 60% 绿色、60%～79.9% 黄色、80%～89.9% 橙色、90% 及以上红色**；配彩色进度条。缺少流量额度不会显示虚假的绿色 0%。
- 到期日直接使用现有节点资料，显示具体日期与剩余/逾期天数。长期节点显示「长期」，未设置显示「未设置到期日」；采用访问设备本地日历日期。
- 新字段仅在原有受保护的只读 Dashboard API 中展示；不改变 Agent 或保护关机/Telegram。**Server v0.4.26-alpha，目标 Agent 仍为 v0.4.22-alpha**，无需集中升级 Agent。

## v0.4.27-alpha：流量使用率简化为三档（Server-only）

- 「流量统计中心」→「服务器流量排名」的百分比进度条仅保留 **三档颜色**：**低于 60% 绿色；60%～90%（包括 90%）黄色；超过 90% 红色**。原有橙色档已经移除。
- 回归测试特别覆盖 60%、90%、90.01% 等边界；订阅日期和剩余天数展示不变。
- **Server v0.4.27-alpha，Agent 目标版本依旧 v0.4.22-alpha**。此调整不改变计费、保护关机、Telegram、历史归档或其他探针功能。

## v0.4.28-alpha：Go 工具链安全升级（Server + Agent）

- **Go 1.23 → Go 1.26.10**。此版本包含 `crypto/x509` 证书链相关安全修复（CVE-2026-111943），要求重新编译的二进制才能受益。
- **Server / Agent 都重新编译**：Server 与 Agent 新版本均为 `0.4.28-alpha`，但**不会自动批量升级 Agent**。管理员需先升级 Server，再通过 SSH 上的 Agent 管理菜单分批确认升级。
- GitHub Actions 固定 Go 1.26.10，自动核查编译产物中嵌入的 Go 版本；发布架构不变（Server amd64/arm64，Agent amd64/arm64/armv7）。
- 安装好的 VPS 不需要额外安装 Go，也不需要 Node.js 或 PHP。本次没有修改流量统计、保护关机、订阅到期、Telegram 或其它业务逻辑。
- CI Action 同步升级至新版 GitHub 托管 Node 运行时；详细验证范围见 `CHANGELOG-v0.4.28-alpha.md`。


## v0.4.29-alpha：订阅管理中心 DEV1（Server-only）

- 新增 Renewlet 风格的中文深色响应式订阅页面及分类、状态、到期时间筛选；通过 HTTPS 和订阅中心独立密码访问。公开 Dashboard 无权访问私人账本。
- SSH 管理菜单新增 **15. 订阅管理中心**，完成非 VPS 订阅的增删改、状态调整以及独立密码设置；目前不开放网页写入。
- 现有 VPS 费用、币种、到期日期直接从节点配置读取，绝不二次录入；私有订阅存储于独立的 0600 JSON 文件。
- Server 版本为 `0.4.29-alpha`，**目标 Agent 仍是 `0.4.28-alpha`**，升级 Server 不要求升级 Agent。
- 使用细则见 `docs/subscriptions-dev1.md`。到期 Telegram 提醒是 DEV2，费用/日历/备份统计是 DEV3，未包含在本版。
