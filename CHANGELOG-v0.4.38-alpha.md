# MiniProbe v0.4.38-alpha — Cloudflare Access 邮箱验证码登录

- 新增 Cloudflare Access 模式，推荐与现有 MiniProbe Cloudflare Tunnel 配合使用：Cloudflare 负责邮箱一次性口令，MiniProbe 不再需要自行发送 SMTP 邮件。
- **必须使用独立订阅 hostname**（示例：`subs.example.com`），保留原 Dashboard/Agent hostname，不拦截 Agent 连接、探针 API、健康检查或已有 Tunnel。
- MiniProbe 服务器使用 Cloudflare 公钥验证每次请求的 `Cf-Access-Jwt-Assertion` **RS256 签名**，同时严格绑定 `iss`（Cloudflare Team）、`aud`（Access 应用）、指定邮箱 `email`、JWT 时效及专用 HTTP hostname。其他身份请求头及 Cloudflare cookie 不能直接授权；公钥缓存 10 分钟，定期更新，校验失败时拒绝访问。
- Access 域名下只需 Cloudflare 的邮箱 PIN；进入订阅中心后可查看、编辑、删除、记录续费；写入仍要求 HTTPS、Origin、CSRF。
- SSH `miniprobe → 15 → 11 → 1` 配置 Access Team、订阅专用域名、应用 AUD、唯一允许邮箱，无需任何 SMTP 授权码；`15 → 11 → 2` 仍保留旧 SMTP 方案（可选）。原有订阅独立密码仍可在普通监控域名或本地 SSH 使用。
- Cloudflare Access 模式的退出登录跳转 `/cdn-cgi/access/logout`；CF Access 负责退出与会话撤销。
- Access 配置独立保存在 Docker 数据卷的 `subscriptions-access.json`（0600）；配置可在本地安全关闭。未启用默认不改变任何行为。
- 新增 RS256 令牌校验与 CSRF 安全测试，包括错 AUD/issuer/email、过期/签名篡改、伪造请求头、跨域与无权限主机。保持 MiniProbe 15 节点规模与现有部署架构不变。
- 提醒：仅提交代码不会自动替用户创建 Cloudflare Zero Trust Access 应用、DNS、Tunnel Public hostname 或 Allow policy；必须按部署文档完成对应 Cloudflare 设置。未配置前绝不可宣称已启用邮箱登录。
