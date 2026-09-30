# MiniProbe v0.4.12-alpha

## New

- 新增独立“线路详情”页面；主 Dashboard 只增加轻量线路状态与入口，不把大型折线图塞进节点卡片。
- 新增最近一天 / 最近一周 / 最近一月三网 RTT 与失败率曲线：24 小时使用 1 分钟粒度，7 天查询时聚合为 5 分钟，30 天查询时聚合为 30 分钟。
- 新增 ASN 路由基准监控：Agent 约每 30 分钟执行一次轻量 ICMP traceroute，Server 将公开跳点映射为 ASN 路径。
- 新增路由变化确认：同一偏离基准的 ASN 路径需连续两次出现才标记为“路由变化”。
- 新增最近 50 条路由变化 / 恢复事件，并在对应运营商 RTT 曲线上标记事件时间，便于关联路由切换与延迟变化。
- SSH 菜单 `11. 国内线路测试` 新增“重置节点线路基准（使用当前 ASN 路径）”。Dashboard 仍保持只读。

## Storage / privacy

- 长期线路历史从主 `miniprobe.json` 分离，按节点 / 日期使用 append-only NDJSON 保存；Server 每分钟聚合一次，不永久写入每 2 秒 Agent Report。
- 原始线路历史最多保留 31 天，独立硬上限 256 MiB；达到上限时优先删除最旧历史，MiniProbe 总存储 2 GiB 预算不变。
- traceroute 原始 hop IP 只作为 Agent -> Server 的瞬时 ASN 映射输入；不写入主数据库、长期线路历史、路由状态文件，也不通过 Dashboard API 暴露。
- ASN 映射集中在 Server，通过 Team Cymru DNS IP-to-ASN 社区服务完成；Agent 不调用第三方 IP intelligence / GeoIP。

## Compatibility

- 保留 v0.4.11-alpha 的 IPv6-only 运营商官网 AAAA + `tcp6/TCP 80` RTT 测量方式。
- IPv4-only / 双栈的北京 / 上海 / 广州三网 ICMP / TCP / UDP 逻辑不变。
- raw ICMP 被宿主环境限制时，仅 ASN 路由区显示不可用；现有节点在线状态、资源监控和 RTT/失败率探测不受影响。
- 本版本修改 Agent Report 与路由采集逻辑，因此 Agent 需要升级到 `v0.4.12-alpha`；可继续使用菜单 12 集中升级。
