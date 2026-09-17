# MiniProbe

MiniProbe 是一个面向个人 VPS 集群的轻量监控探针，重点支持普通 VPS、NAT VPS、IPv6-only VPS，并把线路延迟、丢包和流量安全放在第一优先级。

当前版本：`v0.4.2-alpha`

GitHub：`https://github.com/Jackyhuang83/MiniProbe`

> 设计规模：正常使用 `≤ 15` 个节点。第 16 个节点开始只告警、不强制禁止。
>
> 存储目标：MiniProbe 自身持久化数据有明确上限，不把 1 秒遥测永久写盘；长期运行也不能因为“运行时间越久”无限增长。硬预算为 `2 GiB`。

## 设计原则

- Server 默认直接通过 `IP:28888` 使用，先保证部署简单。
- 可选 Cloudflare Tunnel；验证成功后 MiniProbe 仅监听 `127.0.0.1:28888`，公网 `IP:28888` 不再提供访问。
- Web 只负责查看 Dashboard；节点管理只允许 SSH 登录 Server 后执行 `miniprobe`。
- Agent 主动连接 Server，不需要 Agent 开放任何 MiniProbe 入站端口。
- 无 WebShell、无 SSH 管理、无任意远程命令执行。
- 流量保护关机是 Agent 内置的窄权限保护动作，不提供通用 Shell 执行接口。
- Server 下发给 Agent 的配置使用 Ed25519 签名，Agent 拒绝无效签名和旧版本重放策略。
- 兼容 systemd / OpenRC，目标发行版包括 Debian、Ubuntu、Alpine、Rocky Linux、AlmaLinux、CentOS Stream。

---

# 1. 最简单的使用方式：Direct IP

发布 `v0.4.2-alpha` GitHub Release 后，Server 端默认从该 Release 下载二进制，只需要一条安装命令：

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
 8. 网络接入方式（Direct / Cloudflare Tunnel）
 9. Telegram 通知
10. 存储 / 当前设置
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

无法使用的 IPv4-only Probe 会显示为 `N/A`，不会错误计算成 100% 丢包。

---

# 3. Cloudflare Tunnel

MiniProbe 默认不强迫使用 Cloudflare Tunnel，因为 Direct IP 部署最简单。

确认 Direct 模式工作正常后，可以运行：

```bash
miniprobe
```

选择：

```text
8. 网络接入方式
2. Cloudflare Tunnel
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

如果 Direct 模式仍使用 HTTP，密码传输本身没有 TLS 保护；正式公网使用建议切换 Cloudflare Tunnel / HTTPS。

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
- 国内三网线路延迟 / 失败率（Server 全局选择北京、上海或广州）
- 探测协议可选 ICMP / TCP / UDP
- ICMP：Ping RTT + packet loss
- TCP：TCP/53 connect RTT + connect failure rate
- UDP：UDP/53 DNS query RTT + query failure rate
- Dashboard 分开显示最近 20 次延迟历史与失败/丢包历史（Agent 内部保留小型历史窗口；探测约每 10 秒更新一次）

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

当前这组城市预设目标以 IPv4 为主；IPv6-only Agent 无 IPv4 出口时会把该线路测试显示为 N/A，不影响 CPU / 内存 / 磁盘 / 流量等其它监控。后续只有在找到稳定且城市/运营商归属明确的 IPv6 测试目标后才会加入，避免为了“支持”而混用不可靠节点。

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

# 12. 当前版本说明

`v0.4.2-alpha` 在保持现有安全边界和部署方式不变的基础上，重点优化 Dashboard 的信息层级、紧凑度和隐私显示：

```text
Direct IP 默认部署
Cloudflare Tunnel 可选升级
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
系统信息增加 Debian / Ubuntu / Alpine 等识别图标
月租 / 到期剩余时间进入节点卡片
线路质量按参考模板拆成延迟历史 + 丢包/失败历史两组
线路名包含城市（例如广州联通）
线路协议可在 SSH 菜单全局选择 ICMP / TCP / UDP
线路城市可在 SSH 菜单全局选择北京 / 上海 / 广州
线路区域右上角显示轻量协议标签
```

目前不追求商用规模，也不加入远程服务器管理功能。
