# MiniProbe v0.4.40 — 订阅时区与币种修复

本版基于已发布稳定版 v0.4.39，仅修正 Server 端订阅中心与 VPS 节点菜单的填写体验，不改动 Agent 监控或流量保护行为。

## 更新内容
- 修复精简 Docker 镜像缺少 IANA 时区数据库时，保存订阅报“时区必须是 IANA 时区”的问题。Server 内嵌 `time/tzdata`，不依赖宿主机或容器安装时区包。
- 新增订阅时区下拉：JP（Asia/Tokyo）、US 西部（America/Los_Angeles）、US 东部（America/New_York）、CN（Asia/Shanghai）、DE（Europe/Berlin），支持夏令时。
- 新增及续费币种字段改为可选择的 154 种货币，优先展示常用币种和 INR（印度）、TRY（土耳其）、BRL（巴西）、NGN（尼日利亚）；币种标签含中文名称。既有其他有效时区和历史币种在编辑时保留。
- VPS 节点 SSH 添加/修改时显示 `USD/CNY/HKD/EUR/CAD/GBP` 作为币种填写提示；其中英镑 ISO 代码为 `GBP`。
- 包含对应后端时区、前端交互以及菜单文本回归测试，保留订阅所有人鉴权、CSRF 与私有数据隔离。

## 升级方式与兼容性
- **只需更新 MiniProbe Docker Server**（在宿主机执行 `miniprobe`，菜单 13）；现有 Agent `v0.4.39` 不需要再次升级，Server 的 Agent 目标版本也保持 `0.4.39`。
- GitHub Release 同时包含所需 Server 和 Agent 二进制及 SHA256SUMS；Agent 附带二进制的内部版本仍为 v0.4.39，这是刻意保持兼容的设计。
- GHCR Docker Server 镜像随此版本独立构建；发布不会自行部署到 VPS。更新后进入网页订阅中心测试保存一个 JP / USD 或其他组合即可。
