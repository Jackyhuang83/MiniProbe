# MiniProbe PROJECT_STATE

> 作用：MiniProbe 跨对话交接的唯一“当前状态”文件。
> 每次新建 MiniProbe 对话，开始处理任何日志、测试结果、Bug 或新需求前，先读取本文件，再读取 `MiniProbe-SKILL.md` 中相关章节。
> 本文件只记录“现在做到哪里”；长期架构、安全边界、兼容性和发布规范仍以 `MiniProbe-SKILL.md` 为准。

---

## 1. 当前基线

- 项目：MiniProbe
- 仓库：`https://github.com/Jackyhuang83/MiniProbe`
- 当前已验证发布基线：`v0.4.10-alpha`
- 当前阶段：初版功能基本稳定；除 Bug 或明确新需求外，不主动扩展功能。

---

## 2. 当前正在处理的问题

### IPv6-only 国内三网线路探测端点不具代表性

当前 `v0.4.10-alpha` 对 IPv6-only 节点使用运营商 IPv6 DNS：

```text
电信  240e:4c:4008::1
联通  2408:8888::8
移动  2409:8088::a
```

当前探测方式：

```text
UDP/53 DNS query RTT
```

实机发现 Dashboard 长期出现约 `0-2 ms`。

系统 `dig` 实测：

```text
240e:4c:4008::1 -> 8 ms
2408:8888::8    -> 0 ms
2409:8088::a    -> 0 ms
```

当前判断：

- MiniProbe UDP/53 计时与系统 DNS 查询结果大体一致。
- 问题重点不应先放在计时算法。
- 上述 DNS 可能受 Anycast、透明代理/代答、运营商 DNS 拓扑等影响。
- 这些 RTT 不能可靠解释为 VPS 到中国电信/联通/移动的真实网络路径质量。

已确定原则：

> 先验证新的 IPv6 三网测试端点，不先修改 MiniProbe 探测算法。

---

## 3. 当前 IPv6 三网候选端点实测

### 中国电信

候选：`https://www.189.cn/`

实测：

```text
AAAA: www.189.cn.ctlcdn.cn.
240e:93c:208:4::29
remote=240e:93c:208:4::29
connect=0.187273s
http=412
```

解释：

- AAAA 正常。
- `curl -6` 实际连接 IPv6。
- TCP/HTTPS 建连约 187 ms。
- HTTP 412 不代表 IPv6 不通；更可能是站点/CDN请求策略。

当前状态：候选保留。

### 中国联通

候选 1：`https://www.10010.com/`

```text
AAAA: www.10010.com.eo.dnse0.cn.
240d:c010:18:6::81
remote=240d:c010:18:6::81
connect=0.074312s
http=302
```

候选 2：`https://www.chinaunicom.com.cn/`

```text
AAAA: 855d22aa47.uniwaf.com.
2408:815f:e000:1:18:6:2:2
2408:815f:e000:1:18:6:2:3
2408:815f:e000:1:9:6:2:3
2408:815f:e000:1:8:6:2:3
remote=2408:815f:e000:1:18:6:2:2
connect=0.233068s
http=200
```

当前状态：两者都保留，后续比较长期稳定性、CDN 漂移和代表性。

### 中国移动

候选：`https://www.10086.cn/`

```text
AAAA: 2409:8080:381e:e301::1:14
remote=2409:8080:381e:e301::1:14
connect=0.502384s
http=200
```

当前状态：候选保留。

---

## 4. 当前技术方向

IPv6-only 三网探测正在评估从：

```text
运营商 IPv6 DNS + UDP/53 DNS query RTT
```

转为更能代表真实网络路径的：

```text
运营商官方站点 AAAA
+ 强制 IPv6
+ TCP/HTTPS connect time
```

暂未最终决定：

- 最终三个域名。
- 使用 TCP connect RTT 还是 HTTPS 完整连接时间。
- HTTP 状态码是否只作诊断信息、不参与“可达/不可达”核心判断。
- 是否需要多个候选端点和 fallback。

当前倾向：

- 网络可达性以 DNS AAAA + IPv6 实际连接是否成功为核心。
- `200/301/302/403/412` 等 HTTP 状态本身不应被简单判为网络失败。
- timeout、network unreachable、DNS失败等才属于线路层失败。

---

## 5. 已完成且不要回退的关键基线

- Server + Agent 架构。
- Dashboard 只读。
- Server 管理仅经本机 SSH + `/run/miniprobe/admin.sock`。
- Agent 主动出站，不要求被监控节点开放 MiniProbe 入站端口。
- NAT VPS / IPv6-only VPS 为一等支持对象。
- 正常设计规模 `<= 15` 节点；第 16 个起软告警，不硬限制。
- Cloudflare Tunnel HTTPS 正式部署。
- Server 仅监听 `127.0.0.1:28888` 的正式模式。
- Dashboard 密码保护和 30 天可信设备会话。
- 月流量累计、重置日、计费方向、70/85/90%告警、95%可选保护关机。
- Telegram 掉线、恢复、流量阈值、每 4 小时汇总。
- V4/V6 标签按“是否具备可用出站路由能力”判断，而不是按网卡是否存在地址判断。
- Dashboard V4/V6 与三网探测必须共用同一套地址族可用性逻辑。
- Server 本机 Agent 固定走 `http://127.0.0.1:28888`，不回环公网 Cloudflare/DNS。
- Agent 网络/DNS失败后重建 HTTP Transport。
- 从 `v0.4.9-alpha` 起支持受限、签名校验的 Agent 集中升级。
- `v0.4.10-alpha` 已修复“私网 IPv4 地址但无 IPv4 出站仍误判 V4”的 Bug。

---

## 6. 下一步

当前唯一优先事项：

1. 继续验证电信 / 联通 / 移动 IPv6 官方站点端点。
2. 比较多轮、多节点下的：
   - AAAA 稳定性；
   - IPv6 实际连接成功率；
   - connect time 分布；
   - CDN/Anycast 漂移；
   - 是否真正具备三网线路代表性。
3. 三个端点定稿后，再设计 MiniProbe 探测实现。
4. 在端点未定稿前，不发布新的 Agent 版本。

---

## 7. 跨对话交接协议

当当前 MiniProbe 对话接近长度上限时：

```text
先更新 PROJECT_STATE.md
-> 写入当前版本
-> 写入本轮已完成事项
-> 写入最新实测
-> 写入未解决问题
-> 写入明确的下一步
-> 再建议用户新建对话
```

新对话开始时：

```text
先读取 PROJECT_STATE.md
-> 再读取 MiniProbe-SKILL.md 相关章节
-> 核对项目名 / 当前版本 / 当前问题
-> 再处理用户的新日志或测试结果
```

禁止：

- 仅凭“IPv4/IPv6/VPS”等关键词把当前对话路由到 `vps-bootstrap` 或其他项目。
- 在未核对 MiniProbe 当前状态前，自行猜测用户正在延续哪个项目。
- 要求用户重新复述已经写入 PROJECT_STATE 的项目进度。

---

## 8. 更新规则

`PROJECT_STATE.md` 是滚动状态，不是 CHANGELOG。

每次只保留：

- 当前基线。
- 当前进行中的问题。
- 最新有效测试结果。
- 已确认结论。
- 下一步。

历史版本变化继续写入 CHANGELOG；长期设计规范继续写入 `MiniProbe-SKILL.md`。

---

## 9. 隐私与敏感信息规则

`PROJECT_STATE.md`、`MiniProbe-SKILL.md` 及类似跨对话交接文件只允许保存项目连续性所需的技术信息。

禁止写入：

- 密码、Token、API Key、私钥、Tunnel Token。
- 私人邮箱、手机号、家庭住址、精确位置。
- 私人账号标识、证件号、支付信息。
- 与 MiniProbe 技术工作无关的个人资料或私人生活信息。
- 用户未明确要求长期保留的任何敏感信息。

如果排障日志中临时出现上述内容，交接文件中必须删除、脱敏或改写为占位符，只保留对技术判断必要的信息。
