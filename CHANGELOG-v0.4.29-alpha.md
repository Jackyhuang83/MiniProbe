# MiniProbe v0.4.29-alpha

## 订阅管理中心 DEV1（Server-only）

- 在 Server 集成中文 Renewlet 风格订阅管理中心，包含深色响应式卡片、分类筛选、状态及到期倒计时；兼容 iPhone、iPad、电脑。
- 通过 SSH 菜单 15 登记、编辑、暂停、取消、恢复和删除非 VPS 订阅。删除有确认；所有写入只经过 root-only Unix Socket。
- VPS 自动关联：费用、币种、到期日期实时读取现有 MiniProbe 节点配置，不重复存储或计费；VPS 数据修改仍通过原有节点管理。
- 私有订阅数据单独保存为权限 0600 的 `subscriptions.json`（手动订阅上限 128 条、文件上限 1 MiB），与历史 `miniprobe.json` 分离，降低回滚丢失风险。
- 独立订阅访问密码、HTTPS 要求、仅 24 小时有效的 Secure / HttpOnly / SameSite=Strict Cookie；Public/Protected/Disabled Dashboard 均无法绕过独立授权。
- 无网页写入权限、无新公网端口、不涉及 Snell / SS2022 / Xray / 网络拓扑代理链路。新增针对访问隔离、读写、金额精度、闰年及 VPS 数据关联的回归测试。
- 修复旧 `/ui/0.4.28-alpha/subscriptions.html` 收藏链接向新版订阅页重定向，并为 Release 增加订阅页 JS 语法检查。

### 版本与安装

- Server：`0.4.29-alpha`。
- Agent 目标版本：`0.4.28-alpha`，**没有修改 Agent 业务代码，不要求集中升级 Agent**。
- 仅需在 Server SSH 管理菜单选择 `13. Server 检查更新 / 升级`，并按提示确认。更新前有旧程序备份、SHA256 校验与异常回滚。
- 更新后在 SSH 菜单 15 → 6 设置订阅中心独立密码，网页通过 HTTPS 访问 `/ui/0.4.29-alpha/subscriptions.html`。
- 注意：订阅提醒与续费历史属于 DEV2，统计、日历及数据导入导出属于 DEV3，本次均不包含。

### 安全与升级兼容

- 不修改已安装 Agent 配置、Server 现有节点信息、流量历史、Telegram、Dashboard 设置或生产服务绑定地址。
- 旧 Server 回滚不会自动删除独立订阅账本；但回滚版本没有本新功能。
- GitHub Release 按既有流水线构建 2 个 Server 二进制、3 个 Agent 架构包和 SHA256SUMS；Agent 嵌入版本依旧 `0.4.28-alpha`。
