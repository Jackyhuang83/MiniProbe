# MiniProbe v0.4.43 — Telegram 私人订阅提醒交互按钮

本版基于 v0.4.42，新增 Telegram 原生订阅续费提醒卡片与只读操作按钮，复用现有 MiniProbe Bot Token 和私人订阅中心，无额外 Bot 或服务，不执行任何付款。

## 功能
- 到期提醒展示产品名、类别、实际续费金额、货币、月付/季付/年付周期、到期日期和剩余天数。VPS 年付费用显示真实全年金额。
- Telegram 原生 inline keyboard：💳 续费 / 查看订阅（打开需要登录的 HTTPS 私人订阅中心）；🔄 刷新状态（按 Server 记录更新原消息）；≪ 订阅列表（将原消息替换为最多 10 条有效订阅的只读列表）。
- 刷新和列表只在配置为**私人正整数 Chat ID**时启用；回调核验 Telegram 发送者与原消息私聊的 Chat ID，不响应其他人和群聊的数据访问。
- 提醒默认关闭。复用已配置的 30/7/3/1/0 天、逾期阶段、单独覆写规则、持久化去重和失败重试。
- 使用 Telegram Bot API getUpdates 长轮询处理回调，不新增公网 webhook、管理接口或付费能力。
- 续费按钮只在有可信的 HTTPS 页面地址时显示；Cloudflare Access 专用订阅域名优先。
- 保留原有节点离线/恢复和流量告警，及 v0.4.40–v0.4.42 的全部功能。

## 使用与注意
- 只需在 Server 宿主机 SSH 执行 `miniprobe` → 13，更新 Docker Server。Agent 目标仍为 v0.4.39，不需要升级。
- 确认 `miniprobe` → 9 的 Telegram 设置正常，然后在 `miniprobe` → 15 → 8 启用订阅提醒。到期时自动送达提醒；不主动推送历史订阅。
- 如果该 Bot Token 同时被其他应用通过 webhook/getUpdates 消费，会和按钮回调机制冲突；建议单独用于 MiniProbe。
- 所有续费、更新到期日等写入操作仍须通过受身份验证的私人订阅页面或 SSH 菜单执行。Telegram 不能直接付款或修改 VPS。
- 本次发布不会自行更新生产 VPS，也不会修改仓库或 GHCR 的可见性。
