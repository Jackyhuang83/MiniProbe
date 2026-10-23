# MiniProbe v0.4.35-alpha — 订阅中心 DEV4 网页管理

- 私人订阅中心增加 **添加订阅、编辑、删除、记录续费**，适配 iPhone/iPad 和桌面浏览器。
- 网页仅允许手动订阅的新增、编辑、删除；已有 VPS 节点从探针数据自动同步，网页仅可追加付款记录，不能改写节点配置和到期日。
- 所有网页修改均需通过独立订阅密码登录、HTTPS、Origin 同源校验、JSON Content-Type 与绑定 HttpOnly 登录会话的 CSRF 令牌，未授权及非 HTTPS 请求拒绝写入。
- 只开放作用域受限的订阅写入路由；原有 Unix Socket 管理、密码更改、备份恢复和 Telegram 配置均不通过公共 HTTP 暴露。
- 复用独立 `subscriptions.json`、现有字段验证与原子持久化，续费记录只追加；密码变更会废止旧会话及写入权限。
- 增加 Go 安全回归、浏览器表单/CSRF 单测与 CI；随 Server 版本发布 Linux amd64/arm64 Docker 镜像。
- 不修改已有 Agent、节点、Tunnel、HomeSphere 或 Docker 数据卷。发布新镜像后通过原 Docker 安全更新菜单 13 升级。

注意：订阅中心用于记账和提醒，不执行自动扣款或真实账户凭据存储；私人备份、独立密码修改仍需通过本地管理菜单。
