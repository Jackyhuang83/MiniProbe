---
name: miniprobe
version: 1.0
status: project-skill
description: Maintain, debug, test, package, release, deploy, and operate the MiniProbe personal VPS monitoring project. Use for MiniProbe architecture decisions, Server/Agent code changes, IPv4/IPv6/NAT behavior, domestic line probing, traffic accounting, Dashboard/Telegram behavior, Agent self-update, GitHub release workflows, production troubleshooting, and step-by-step user operations. Preserve MiniProbe's strict security, storage, compatibility, and simplicity constraints.
repository: https://github.com/Jackyhuang83/MiniProbe
baseline: v0.4.11-alpha
language: zh-CN
---

# MiniProbe Skill

MiniProbe 是一个面向个人 VPS 集群的轻量级监控探针。核心目标不是做“功能最多”的服务器面板，而是以很小的资源和磁盘成本，稳定监控少量 VPS，并优先解决：节点在线状态、资源状态、月流量安全、国内三网线路质量、IPv6-only / NAT VPS 兼容、可靠恢复以及安全运维。

本 Skill 用于后续继续维护 MiniProbe。执行任何 MiniProbe 任务时，应优先保持现有设计边界和已经验证过的工作流，不应因为“实现起来方便”而引入远程 Shell、无限历史、第三方 IP 情报依赖、复杂控制面或不必要的新功能。

---

## 1. 项目任务目标

MiniProbe 采用 Server + Agent 架构。

Server 负责：

- 接收 Agent 主动上报。
- 保存节点配置和最新状态。
- 提供只读 Dashboard。
- 通过本机 SSH 菜单 `miniprobe` 完成管理。
- 提供 Telegram 通知。
- 下发 Ed25519 签名策略。
- 提供 Agent 二进制及受限的集中升级能力。

Agent 负责：

- 主动连接 Server，不开放 MiniProbe 入站端口。
- 采集 CPU、内存、磁盘、网络、流量、系统信息。
- 执行国内线路探测。
- 本地持久化月流量状态。
- 按签名策略更新配置。
- 在满足严格校验条件时执行 MiniProbe Agent 自更新。
- 在用户明确启用保护关机时执行固定的本机关机动作。

项目适用对象：

- 普通 IPv4 VPS。
- NAT VPS。
- IPv6-only VPS。
- IPv4 / IPv6 双栈 VPS。
- 小规模个人 VPS 集群。

正常设计规模：

```text
<= 15 个节点
```

第 16 个节点开始只做软告警，不硬限制。

---

## 2. 当前基线

当前已验证发布基线：

```text
v0.4.11-alpha
```

当前已经完成并经过实机验证的重要能力：

- Server / Agent 基本监控链路。
- Cloudflare Tunnel HTTPS。
- Server 仅监听 `127.0.0.1:28888` 的正式部署方式。
- Dashboard 密码保护和 30 天可信设备会话。
- NAT VPS / IPv6-only VPS 支持。
- 月流量累计、重置日、计费方向、95% 可选保护关机。
- Telegram 掉线、恢复、流量阈值、定时摘要。
- 国内三网延迟 / 丢包或失败率测试。
- V4 / V6 简化网络标签。
- Server 本机 Agent 固定走 loopback，避免 Cloudflare/DNS 回环依赖。
- Agent 网络/DNS失败后重建 HTTP Transport。
- 从 v0.4.9-alpha 起支持 Agent 集中升级。
- v0.4.10-alpha 修复“私网 IPv4 地址但无 IPv4 出站能力仍被误判 V4”的问题。
- v0.4.11-alpha 修复 IPv6-only 三网 DNS RTT 失真：改为运营商官网 AAAA + 强制 tcp6/TCP 80 connect RTT。

当前产品策略：

> 初版功能已基本稳定。除非出现新的 Bug 或明确的新需求，不主动扩展新功能。

---

## 3. 不可突破的产品边界

### 3.1 禁止变成远程管理面板

MiniProbe 是监控探针，不是服务器控制面板。

禁止加入：

- WebShell。
- 远程 SSH。
- 任意远程命令执行。
- 通用脚本下发。
- 文件管理器。
- 通用服务启停。
- 通用软件安装。

Agent 的远程动作只能是提前定义、边界明确、可验证的固定动作，例如：

- 接收签名配置。
- 接收签名 Agent 更新策略。
- 达到用户配置的保护阈值后执行固定关机动作。

不得把集中升级设计成“Server 下发 Shell 字符串”。

### 3.2 Dashboard 必须只读

Dashboard 只展示监控数据。

任何以下管理动作必须经 Server 本机 SSH：

```text
SSH
  -> Server VPS
  -> miniprobe
  -> /run/miniprobe/admin.sock
```

公网不提供管理 API。

Unix Socket：

```text
/run/miniprobe/admin.sock
权限 0600
```

### 3.3 Agent 必须主动连接

Agent 不能要求用户在被监控 VPS 上开放 MiniProbe 端口。

NAT VPS 必须是一等支持对象。

### 3.4 不依赖第三方 IP 情报做网络属性判断

Dashboard 网络标签只保留：

```text
V4
V6
```

不要恢复以下标签：

```text
V4 NAT
IDC
家宽
移动
原生
广播
```

原因：这些推断已经实测不可靠，而且增加第三方依赖。

如果没有其他功能需要，不应重新加入 GeoIP / IP intelligence / RDAP 外部查询。

---

## 4. 安全约束

### 4.1 Agent Token

- 高熵 Bearer Token。
- 不放 URL Query。
- 不在回复中重复用户 Token。
- 不要求用户把完整 Agent 安装命令贴回聊天，因为命令中可能包含 Token。
- 不要求用户贴 Dashboard 密码、Cloudflare Tunnel Token 或其他秘密。

### 4.2 策略签名

Server -> Agent 配置必须通过 Ed25519 签名。

Agent 应拒绝：

- 无效签名。
- 目标 Node ID 不匹配。
- 旧版本重放策略。
- 不合法字段。

### 4.3 集中升级安全边界

集中升级不是远程 Shell。

Server 只能下发固定 `UpgradePolicy`，至少约束：

- Node ID。
- Request ID。
- Target Version。
- 当前架构允许的 Agent Asset。
- 文件大小。
- SHA256。
- 签名。

Agent 更新时至少执行：

```text
确定自身 CPU 架构
-> 获取固定 Agent 文件名
-> 下载临时文件
-> 校验大小
-> 校验 SHA256
-> 校验签名策略
-> 执行新二进制 --version 自检
-> 原子切换
-> 重启 / 重新执行
-> 上报新版本
```

禁止：

- 任意下载 URL。
- 任意 Shell。
- 任意目标路径。
- 更新时覆盖或重写 Token。
- 更新时无校验直接替换。

### 4.4 TLS

正式公网部署优先：

```text
Cloudflare Tunnel + HTTPS
```

Direct HTTP：

```text
http://IP:28888
```

只用于：

- 首次部署。
- 故障恢复。
- 临时诊断。

Direct HTTP 不能被描述成安全的长期公网部署方式。

---

## 5. 存储与日志硬约束

MiniProbe 曾明确针对小硬盘 VPS 设计，必须避免类似旧监控系统数月跑满 5 GB 磁盘的问题。

硬目标：

```text
MiniProbe 持久化总量长期不得超过 2 GiB
```

设计原则：

- 保存最新状态，而不是永久保存每秒遥测。
- 只保存小窗口线路历史。
- 不做无限增长时序数据库。
- 不保存每次 Agent report 的日志。
- 正常上报不要逐条写日志。
- 连续失败日志应限频。
- systemd 使用日志速率限制。
- OpenRC 不创建无限增长日志文件。

优先级：

> 宁可丢失高频历史，也不能让 MiniProbe 把 VPS 磁盘写满。

数据库文件应维持额外的低安全上限；历史设计目标为约 256 MiB 级别，而不是靠 2 GiB 总预算兜底。

---

## 6. 平台兼容约束

目标 Linux：

- Debian
- Ubuntu
- Alpine
- Rocky Linux
- AlmaLinux
- CentOS Stream

服务管理：

- systemd
- OpenRC

Agent 架构：

```text
linux/amd64
linux/arm64
linux/armv7
```

Server 架构：

```text
linux/amd64
linux/arm64
```

所有新增代码必须考虑：

- Alpine / BusyBox shell 差异。
- OpenRC。
- 无 systemd 环境。
- IPv6-only 下载环境。
- NAT VPS。

不要只在 Ubuntu + systemd 上验证后就认为完成。

---

## 7. 网络地址族判定语义

这是 MiniProbe 的重要约束。

### 7.1 V4 / V6 表示什么

V4 / V6 表示：

> 本机内核是否具备该地址族的可用出站路由能力。

不是：

> 网卡配置里是否存在某个 IPv4 / IPv6 地址。

### 7.2 已发现的真实 Bug

IPv6-only VPS 曾存在：

```text
eth0 = 10.10.10.10/32
IPv4 route = 空
IPv6 可正常出网
```

旧逻辑看到 `10.10.10.10` 后错误标记 `V4`，并把节点当成双栈，导致三网测试继续访问 IPv4 目标，全部 timeout。

v0.4.10-alpha 已改为路由能力检测。

当前实现的语义近似：

```text
udp4 connect 1.1.1.1:53
udp6 connect [2606:4700:4700::1111]:53
```

Go 的 UDP connect 在此主要用于让内核做路由 / 源地址选择，不依赖远端返回数据。

因此：

```text
10.x.x.x + 无 IPv4 出站路由    -> 不显示 V4
10.x.x.x + 有 IPv4 默认/出站路由 -> 显示 V4
IPv6 有可用出站路由             -> 显示 V6
```

### 7.3 必须共用同一套判断

以下功能必须共用地址族可用性逻辑：

- Dashboard V4 / V6 标签。
- 三网探测选择 IPv4 还是 IPv6 目标。

禁止两个模块分别实现一套“IPv6-only 判断”。

---

## 8. 国内线路测试

入口：

```text
miniprobe
-> 11. 国内线路测试（北京 / 上海 / 广州）
```

IPv4-only / 双栈节点当前城市目标：

```text
北京
电信 219.141.136.10
联通 202.106.0.20
移动 221.130.33.60

上海
电信 202.96.209.133
联通 210.22.70.3
移动 211.136.112.50

广州
电信 202.96.128.86
联通 210.21.4.130
移动 211.136.192.6
```

支持协议：

```text
ICMP
TCP/53
UDP/53 DNS query
Off
```

探测周期约：

```text
10 秒/轮
```

每轮：

```text
4 次尝试
```

历史：

```text
最近 20 轮
最多对应 80 次尝试
```

右侧丢包/失败率必须与 20 轮历史窗口一致，不应只显示最后一轮 4 次的瞬时百分比。

### 8.1 IPv6-only 当前逻辑

从 v0.4.11-alpha 起，IPv6-only 不再使用运营商公共 DNS RTT 作为三网线路基准。

当前端点：

```text
电信 www.189.cn
联通 www.chinaunicom.com.cn
移动 www.10086.cn
```

当前探测方式：

```text
解析 AAAA
-> 强制 tcp6
-> TCP/80 connect
-> 仅统计 TCP connect RTT
-> 立即关闭连接
```

不发送 HTTP 请求，不进行 TLS 握手；HTTP 状态码、证书和 TLS renegotiation 均不属于线路健康判定。

DNS 解析不计入 RTT。域名只用于跟随运营商官网当前 IPv6/CDN 端点；实际延迟计时从 TCP connect 开始。

Dashboard 行名：

```text
IPv6电信
IPv6联通
IPv6移动
```

协议显示：

```text
TCP
```

不要伪装成北京 / 上海 / 广州城市节点，因为官网可能使用运营商 CDN/WAF，不能声称具体城市精度。

### 8.2 v0.4.10-alpha 历史问题与修复依据

v0.4.10-alpha 曾使用：

```text
电信 240e:4c:4008::1
联通 2408:8888::8
移动 2409:8088::a
```

并以 UDP/53 DNS query RTT 测量。实机长期出现约 `0-2 ms`；系统 `dig` 也出现 `0 ms`，说明计时算法与系统工具大体一致，但 DNS Anycast、透明代答或网络拓扑使该数值不能代表三网真实路径。

经多轮 IPv6 实测后，运营商官网端点的 TCP 建连延迟呈现更合理、可重复的跨网区间；因此 v0.4.11-alpha 改为官网域名 + AAAA + tcp6/TCP 80 connect RTT。

保留原则：

- DNS 只负责发现当前 IPv6 端点，不进入 RTT 计时。
- TCP 建连成功即视为本次线路探测成功；不依赖 HTTP 200。
- DNS AAAA 失败、IPv6 无路由、TCP timeout/refused 才按失败统计。
- 每轮仍执行 4 次尝试，失败率仍按最近 20 轮窗口统计。
- 双栈节点继续沿用城市 IPv4 基准和用户选择的 ICMP/TCP/UDP 协议，不改变历史口径。

---

## 9. Server 本机 Agent 特殊规则

Server VPS 自己也可能运行 Agent。

曾发生实际故障：

```text
Server 重启 / 网络恢复
-> Cloudflare Tunnel 恢复
-> Dashboard 可访问
-> Server 本机 Agent 仍通过公网 hostname 回环
-> DNS lookup timeout
-> Agent 长时间保持离线
```

因此 Server 本机 Agent 不应走：

```text
Server Agent
-> 公网 DNS
-> Cloudflare
-> cloudflared
-> 127.0.0.1
-> Server
```

正确路径：

```text
Server Agent
-> http://127.0.0.1:28888
-> Server
```

当前识别方式使用：

- `/run/miniprobe/admin.sock` 存在。
- `http://127.0.0.1:28888/healthz` 成功。

确认是 Server 本机后，只使用 loopback，不应再 fallback 到公网 hostname。

Remote Agent 继续使用公网 HTTPS Endpoint。

Loopback HTTP 是同机通信特例，不代表放宽远程 TLS 原则。

---

## 10. 月流量语义

Agent 状态：

```text
/var/lib/miniprobe-agent/state.json
```

首次安装：

- 以当前内核网卡计数作为 baseline。
- 不追溯 MiniProbe 安装之前的流量。

因此月中安装时：

- MiniProbe 只知道安装后流量。
- 云厂商本月更早流量仍以供应商控制台为准。

重启行为：

- Agent 重启不归零。
- VPS 重启不主动归零。
- 内核 counter 回退/重置时保留已累计值并建立新 baseline。

计费方向：

```text
outbound
inbound
total
```

单位：

```text
1 GB = 1,000,000,000 bytes
```

不要改成 GiB。

默认告警：

```text
70%
85%
90%
```

可选保护关机默认参考阈值：

```text
95%
```

保护关机必须由用户明确启用。

---

## 11. 节点资料局部修改规则

入口：

```text
miniprobe
-> 3. 修改节点 / 流量策略
```

交互规则：

```text
直接回车 = 保持原值
-        = 清空支持清空的可选字段
```

不要强迫用户重新输入所有配置。

支持字段包括：

- 月流量 GB。
- 流量方向。
- 重置日。
- 时区。
- 保护关机。
- 月租。
- 币种。
- 到期日。
- 标签。

长期节点：

```text
L
long
longterm
长期
```

可表示长期续费。

内部兼容标记：

```text
2036-01-01
```

Dashboard 显示：

```text
长期
```

不要显示成大量剩余天数。

---

## 12. Dashboard UI 约束

视觉方向：

- 深色蓝灰。
- 紧凑 VPS 卡片。
- 不追求大面积留白。
- 保留 card-within-card 的层级结构。
- 资源区保持紧凑 2x2。
- 实时流量、计费流量、套餐信息分区清晰。
- 网络质量区域紧凑。
- OS / arch / KVM 属于低优先级信息。
- 价格和剩余天数可见。
- 未设置配额显示 `未设置配额`。

不要为了“现代化”而把嵌套卡片全部拍平。

系统信息行示例：

```text
Debian 12 · KVM · amd64 · V4
Debian 12 · KVM · amd64 · V6
Debian 12 · KVM · amd64 · V4 · V6
```

不显示节点公网 IP。

---

## 13. Telegram 约束

默认掉线阈值：

```text
2 分钟
```

恢复后发送恢复通知。

流量告警：

```text
70%
85%
90%
```

定时摘要：

```text
4h 默认推荐
8h
12h
24h
off
```

多个节点应尽量合并成一条摘要，不要逐节点刷屏。

第 16 个节点跨越软上限时，如果 Telegram 已开启，应发送一次提示。

---

## 14. Agent 集中升级

入口：

```text
miniprobe
-> 12. Agent 版本管理 / 集中升级
```

状态示例：

```text
最新
需手动一次
可集中升级
升级中
离线/可排队
```

历史一次性限制：

```text
v0.4.8-alpha 及更早 Agent
-> 不具备自更新代码
-> 首次进入 v0.4.9-alpha 必须手动一次
```

当前所有已部署 Agent 已完成这一 bootstrap。

因此从 v0.4.9-alpha 之后：

```text
发布新版本
-> 升级 Server
-> miniprobe
-> 12
-> 一键升级全部可升级 Agent
```

不再逐台 SSH。

### 14.1 不要求 Server 和 Agent 每次同版本

后续版本管理应区分：

```text
Server Version
Required Agent >=
Recommended Agent
```

如果改动仅涉及：

- Dashboard CSS。
- Server 菜单。
- Telegram 文案。
- Server-only 逻辑。

不应强制 Agent 一起升级。

如果改动涉及：

- Agent 采集。
- Agent 网络判断。
- Agent 探测。
- Agent 流量算法。
- Agent 升级逻辑。

才需要 Agent 更新。

---

## 15. GitHub 与发布工作流

仓库：

```text
https://github.com/Jackyhuang83/MiniProbe
```

生产分支：

```text
main
```

### 15.1 版本规则

版本递增，不覆盖旧版本。

开发 / 初版阶段使用：

```text
vX.Y.Z-alpha
```

每个版本必须同步更新：

- `Makefile` 中 VERSION。
- Server Version。
- Agent Version。
- Installer 默认 VERSION。
- `CHANGELOG-vX.Y.Z-alpha.md`。
- README 当前版本说明。

### 15.2 源码 ZIP 命名

```text
MiniProbe-vX.Y.Z-alpha-github-ready.zip
```

ZIP 应为 GitHub Import workflow 可直接解包覆盖源码的完整包。

### 15.3 固定用户发布流程

每次交付都按以下完整流程带用户操作，不要求用户记忆：

```text
1. 下载版本 ZIP
2. GitHub 上传 ZIP + Commit
3. 运行 Import MiniProbe Source Package
4. 等 Import 绿勾
5. 验证 VERSION / Agent Version / CHANGELOG / 关键代码
6. 运行 Release MiniProbe Current Version
7. 等 Release 绿勾
8. 验证 Release / assets / installer 默认版本
9. 升级 Server
10. 验证 service active + /healthz = ok
11. 如 Agent 需要升级：miniprobe -> 12 -> 集中升级
12. 实机验证本次 Bug / 功能
```

固定检查点用语：

```text
ZIP 已提交
Import 绿勾
Release 绿勾
```

### 15.4 Import workflow

永久工作流：

```text
.github/workflows/import-miniprobe.yml
```

名称：

```text
Import MiniProbe Source Package
```

其职责：

- 找最新根目录 `MiniProbe-v*-github-ready.zip`。
- 解压到临时目录。
- rsync 覆盖源码。
- 排除 `.git/` 和 `.github/`。
- 删除根目录 ZIP。
- github-actions[bot] Commit + Push。
- 保留 `.github` 工作流自身。

### 15.5 Release workflow

永久工作流：

```text
.github/workflows/release-miniprobe.yml
```

名称：

```text
Release MiniProbe Current Version
```

其职责：

- checkout。
- 根据 go.mod setup Go。
- 从 Makefile 读取 VERSION。
- 找对应 CHANGELOG。
- `go test`。
- `go vet`。
- `make release`。
- 创建 GitHub Release。
- alpha/beta/rc 标记 prerelease。
- 上传 5 个二进制 + SHA256SUMS。

不要恢复旧的硬编码版本 workflow，例如：

```text
release-v041.yml
```

---

## 16. 交付前的代码工作流

任何 Bugfix / 新版本都遵循：

### Step A - 明确问题证据

先确认实际现象，不凭猜测修改。

优先收集：

- 用户实机输出。
- 当前源码。
- Server / Agent 日志。
- 系统命令对照测试。
- 旧版与新版行为差异。

如果一个现象可能有多个根因，应先设计区分度高的最小测试。

### Step B - 定位根因

必须把：

```text
观察到的现象
推断
已验证事实
```

分开。

不能因为“看起来像”就声称根因已经确认。

### Step C - 最小修改

初版稳定后采用：

> 一个 Bugfix 版本只修改必要逻辑，不顺便加入不相关功能。

优先修改共享底层逻辑，避免多个模块分别打补丁。

例如 V4/V6 判断应由一个统一函数提供给：

- Dashboard 标签。
- 探测地址族选择。

### Step D - 回归测试

至少执行：

```bash
go test ./...
go vet ./...
```

Shell：

```bash
sh -n scripts/*.sh
```

构建：

```bash
make release
```

验证产物至少包括：

```text
miniprobe-server-linux-amd64
miniprobe-server-linux-arm64
miniprobe-agent-linux-amd64
miniprobe-agent-linux-arm64
miniprobe-agent-linux-armv7
SHA256SUMS
```

如版本涉及 Agent，至少执行一个新 Agent 二进制：

```bash
./dist/miniprobe-agent-linux-amd64 --version
```

版本号必须匹配。

### Step E - 针对性回归

不能只跑编译测试。

Bugfix 必须设计针对根因的专项验证。

示例：IPv6-only V4 误判 Bug：

```text
私网 IPv4 地址存在
IPv4 无出站路由
IPv6 有出站路由
```

预期：

```text
V4 = false
V6 = true
IPv6-only 三网分支生效
```

### Step F - 打包后再次验证

不能只验证工作目录。

必须：

```text
生成 github-ready.zip
-> 解压到新的临时目录
-> 在解压副本重新跑关键测试/检查
```

防止漏文件、旧文件、打包路径错误。

### Step G - 计算 SHA256

向用户交付 ZIP 时提供：

```text
文件名
文件大小
SHA256
```

### Step H - 绝不虚构结果

只有工具真实执行后才可以说：

```text
已创建
已测试
测试通过
已上传
GitHub 已发布
Release assets 完整
```

如果没有执行，就必须明确写成“计划”“建议”“待验证”。

---

## 17. 生产升级流程

Server 安装 / 升级标准命令：

```bash
curl -fsSL https://raw.githubusercontent.com/Jackyhuang83/MiniProbe/main/scripts/install-server.sh | bash && \
sleep 2 && \
echo "服务状态: $(systemctl is-active miniprobe-server)" && \
echo -n "Health: " && curl -fsS http://127.0.0.1:28888/healthz && echo
```

预期：

```text
服务状态: active
Health: ok
```

如果是 OpenRC 环境，不应硬套 systemctl 检查；应使用对应服务管理命令。

Agent v0.4.9+ 后优先集中升级。

只有以下情况才逐台手动：

- Agent 版本太旧，不支持 self-update。
- Agent 自更新机制本身损坏。
- 节点无法连接 Server。
- 需要恢复性安装。

手动更新时，应尽量复用 `/opt/miniprobe/agent.env` 中已有配置，不要求用户重新查 Token。

不得要求用户把 `agent.env` 内容复制回聊天。

---

## 18. 故障排查标准顺序

### 18.1 节点离线

按顺序确认：

```text
1. Agent service 是否运行
2. Server /healthz 是否可达
3. Agent 当前 endpoint
4. DNS 是否正常
5. Cloudflare Tunnel 是否正常
6. Agent 日志中的实际错误
7. state.json 是否保留旧 endpoint
```

不要一上来就重装。

### 18.2 Server 本机 Agent 离线

优先确认：

```text
/run/miniprobe/admin.sock
http://127.0.0.1:28888/healthz
```

Server 本机 Agent 应走 loopback。

### 18.3 三网全 timeout

先区分：

- IPv4-only。
- IPv6-only。
- 双栈。

确认地址族判定，再确认目标和协议。

IPv6-only 当前固定使用运营商官网 AAAA + tcp6/TCP 80；排障时先检查 AAAA、IPv6 出站路由和 TCP/80，不再使用运营商 DNS RTT 作为线路基准。

### 18.4 延迟异常低

不要直接认定代码 Bug。

做系统工具对照：

- `dig`
- `ping -6`
- TCP 连接测试
- 必要时 traceroute / nexttrace

如果 MiniProbe 与系统工具一致，应怀疑测试端点语义，而不是计时器。

### 18.5 流量异常

确认：

- `state.json` 是否存在。
- 网卡选择是否正确。
- 是否首次安装导致 baseline 从当前时刻开始。
- 内核 counter 是否重置。
- 计费方向是否与云厂商一致。
- 云厂商是否使用十进制 GB。

---

## 19. 用户交互原则

用户不是以“学习 Git / Go / Linux 运维流程”为目标，而是希望把项目稳定做完。

因此操作指导必须：

- 中文。
- 精确。
- 一次给当前动作。
- 等用户确认后再进入下一步。
- 不要求用户记住之前流程。
- 每次发布都重新给完整操作。
- 尽量复制粘贴即可执行。
- 不使用含糊的“你应该知道”“跟之前一样”。
- 不为了显得专业而增加无关术语。

可以把紧密相关的微步骤放在一次操作里，例如：

```text
上传 ZIP + Commit
```

不要拆成：

```text
先选文件
等回复
再点 Commit
```

但重大检查点要分开：

```text
ZIP 已提交
-> 验证
-> Import
-> 验证
-> Release
```

### 19.1 操作回复标准格式

操作型回答建议结构：

```text
先说明当前状态 / 为什么做这一步。

现在只做这一件事：
<明确路径或命令>

正常结果：
<预期输出>

完成后回复：
<固定短语>
```

不要一次塞给用户五六个阶段的操作，除非用户明确要求“一次全部发给我”。

### 19.2 诊断回复标准格式

诊断型回答建议：

```text
1. 先给当前结论级别：已确认 / 高度怀疑 / 尚不能确认。
2. 用一条因果链解释为什么。
3. 给一个最小区分测试。
4. 要求用户复制文本结果，不优先要求截图。
```

示例：

```text
10.10.10.10/32 存在
-> 旧逻辑判定 hasV4
-> 实际 IPv4 route 为空
-> VPS 实际 IPv6-only
-> 三网错误走 IPv4
-> timeout
```

### 19.3 交付回复标准格式

代码包交付必须写：

```text
版本号
本版只改了什么
测试项及真实结果
文件下载链接
文件大小
SHA256
下一步只做什么
```

不要声称未执行的测试已经通过。

---

## 20. GitHub 操作验证原则

如果可访问 GitHub API / Connector，应在用户每个关键检查点后实际验证，不只相信口头反馈。

### ZIP 已提交后

验证：

```text
文件名
size
blob SHA
```

### Import 绿勾后

验证至少：

```text
Makefile VERSION
Agent version
CHANGELOG 存在
本次关键代码已导入
```

### Release 绿勾后

验证：

```text
Tag
Release name
Draft=false
Prerelease 是否正确
6 个 assets 是否齐全
各 asset size/digest
installer 默认版本
```

不要在未验证时说“Release 完整成功”。

---

## 21. 当前 Release 资产规范

每个正式 Release 应包含：

```text
miniprobe-agent-linux-amd64
miniprobe-agent-linux-arm64
miniprobe-agent-linux-armv7
miniprobe-server-linux-amd64
miniprobe-server-linux-arm64
SHA256SUMS
```

如缺任何一个，应视为 Release 不完整。

---

## 22. 版本变更策略

Bugfix 优先采用小版本递增，例如：

```text
v0.4.10-alpha
-> v0.4.11-alpha
```

不要覆盖已发布 Tag。

不要为了一个小 Bug 大范围重构。

如果只是 Server-only 改动：

- 可以只升级 Server。
- Agent 不必为了“版本号一致”而更新。

如果 Agent 需要更新：

- Release 后先升级 Server。
- 使用菜单 12 集中升级。
- 再通过菜单 12 确认版本状态。

---

## 23. 当前菜单基线

```text
============================================================
                     MiniProbe
============================================================
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

新增菜单项前必须评估是否真的属于监控探针职责。

---

## 24. README / CHANGELOG 要求

README 应作为当前行为说明，而不是堆叠开发过程。

README 中需保持：

- 当前版本。
- 安装方式。
- Server / Agent 架构。
- 安全边界。
- Dashboard 模式。
- NAT / IPv6-only 行为。
- 流量语义。
- Telegram。
- 存储上限。
- 集中升级。
- 当前已验证的重要限制。

CHANGELOG 只写当前版本变化，重点说明：

```text
Fix
Change
Compatibility
Upgrade notes
```

不要把未完成或未验证功能写成已交付。

---

## 25. 代码修改检查清单

提交任何 MiniProbe 版本前逐项检查：

- [ ] 是否仍无公网管理 API？
- [ ] 是否仍无远程 Shell？
- [ ] Dashboard 是否仍只读？
- [ ] Agent 是否仍主动出站？
- [ ] NAT VPS 是否未被破坏？
- [ ] IPv6-only 是否未依赖 IPv4 下载？
- [ ] systemd 是否正常？
- [ ] OpenRC 是否正常？
- [ ] amd64 / arm64 / armv7 是否按设计构建？
- [ ] V4 / V6 是否按可用出站路由判断？
- [ ] 三网探测是否与 V4 / V6 共用地址族判断？
- [ ] Server 本机 Agent 是否仍固定 loopback？
- [ ] Agent 状态文件是否保持兼容？
- [ ] 月流量是否不会升级后归零？
- [ ] 新增日志是否会无限增长？
- [ ] 持久化数据是否仍受 2 GiB 总预算约束？
- [ ] Agent 更新是否仍只接受签名固定策略？
- [ ] 是否避免第三方 IP 情报依赖？
- [ ] README / CHANGELOG / VERSION 是否同步？
- [ ] `go test ./...` 是否真实执行？
- [ ] `go vet ./...` 是否真实执行？
- [ ] `make release` 是否真实执行？
- [ ] ZIP 解压副本是否再次检查？
- [ ] SHA256 是否重新计算？
- [ ] 是否只声称真实验证过的结果？

---

## 26. 不要做的事情

除非用户明确改变需求，否则不要：

- 加 WebShell。
- 加远程 SSH。
- 加“执行任意命令”功能。
- 变成服务器运维面板。
- 引入大型数据库保存长期秒级历史。
- 为网络标签重新接 IP intelligence API。
- 把 NAT/IDC/家宽/原生/广播重新放回 UI。
- 仅因为网卡有私有 IPv4 就显示 V4。
- 恢复使用 IPv6-only 公共 DNS RTT 作为真实三网路径基准；v0.4.11-alpha 已改为验证过的运营商官网 tcp6/TCP 80 connect RTT。
- 把 NextTrace endpoint 自动当成稳定 Ping endpoint。
- 每个 Server 版本都强制升级 Agent。
- 覆盖旧 Release / Tag。
- 让用户每次重新找 Token。
- 要求用户把 Token / Password / Tunnel Token 发回来。
- 在没有工具执行证据时声称代码已完成、测试已通过或 Release 已成功。
- 一次给用户过多独立运维步骤，导致操作容易出错。

---

## 27. 项目收口后的默认维护策略

MiniProbe 当前进入“稳定优先”阶段。

默认优先级：

```text
P0 安全问题 / 数据损坏 / 流量保护错误
P1 节点掉线 / 无法恢复 / 地址族误判 / 探测完全失效
P2 Dashboard 错误显示 / Telegram 错误通知 / 统计口径错误
P3 UI 微调 / 易用性优化
P4 新功能
```

除非用户主动提出，新功能排在 Bugfix 之后。

出现 Bug 时：

```text
先复现
-> 找根因
-> 最小修复
-> 自动测试
-> 针对性实测
-> 打包
-> GitHub Import
-> Release
-> Server 升级
-> 必要时 Agent 集中升级
-> 实机回归
```

这条流程是 MiniProbe 后续维护的默认工作方式。
