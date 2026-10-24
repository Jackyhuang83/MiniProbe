# Cloudflare Access 邮箱验证码登录 — MiniProbe v0.4.38-alpha

> Cloudflare Access 发送验证码；MiniProbe **不发送邮件、不要 SMTP 授权码，也不接收 Cloudflare API Token**。原独立密码登录和 SSH 本地密码恢复保持可用。

## 为什么使用独立子域名？
现有 MiniProbe 正通过 Cloudflare Tunnel 对外提供 Dashboard 和 Agent 通信。如果对整个现有域名启用 Cloudflare Access，Agent/探针会被重定向到邮箱登录页而失联。应只为私人订阅中心使用一个**新的 hostname**（示例 `subs.example.com`），原 hostname 保持不变。

## 首次设置：Cloudflare One（一次性）

1. 登录 Cloudflare Dashboard 的 **Zero Trust**，在 **Integrations → Identity providers** 确认启用了 **One-time PIN**（OTP）。新建账户未必默认包含 OTP。
2. 进入现有 **Networks → Tunnels**，在 MiniProbe 现有 Tunnel 的公开主机名（Public hostnames）中**新增** `subs.example.com`，服务地址与已有 MiniProbe Server 一致。**不要修改、删除原公开主机名**；不需要增加 Docker 端口或第二套容器。如果 cloudflared 现有公共域名映射到哪个内部服务，请直接复用**同一服务地址**，不要猜端口。
3. 在 **Access controls → Applications → Add an application → Self-hosted** 创建应用，应用 Domain 指向**整个新订阅 hostname** `subs.example.com`，不要只选一个子路径。Session duration 推荐 24 小时或更短。
4. 创建 **Allow** Policy，Include 规则选择 **Emails**，填写**你自己的唯一允许邮箱地址**；不要选择 Everyone，也不要仅仅选择 Login Methods: One-time PIN（会放行任意有邮箱的用户）。如果需要更多人访问，应明确再做权限设计，而不是开放整个域。
5. 从该 Access 应用设置复制 **Application Audience (AUD) Tag**，记录 Cloudflare One 的 **Team name**（即 `https://<team>.cloudflareaccess.com` 中的 `<team>`）。不需要 API Token。

## MiniProbe 本地配置

等新版 GHCR Docker 镜像发布并在 SSH 升级后：
1. SSH 输入 `miniprobe`，选择 **15 → 11 → 1 → 1**（邮箱登录设置 / Cloudflare Access / 启用）。
2. 填写专用 hostname（如 `subs.example.com`）、Team name、AUD Tag 和唯一允许邮箱。首次启用只配置一次，后续网页登录只输入邮箱。
3. 系统联网读取 `https://<team>.cloudflareaccess.com/cdn-cgi/access/certs`，必须成功取得 Cloudflare RS256 公钥，才保存配置。此检查**无法证明** Cloudflare 域名/Allow policy 已正确设置，仍需网页实测。
4. 打开 `https://subs.example.com/subscriptions.html`：Cloudflare Access 要求输入邮箱，收到验证码后填写即可。验证成功，MiniProbe 自动读到经过签名的身份，不再要求第二个订阅密码。
5. 退出点击「安全退出」，跳转到 Cloudflare `/cdn-cgi/access/logout`。

## 安全验收与回滚
- 未经 Cloudflare 邮箱登录，新域名**不能**读取私人订阅 API；伪造邮箱头、Dashboard cookie、普通独立密码都不能绕过新域名上的 JWT 验证。
- 普通监控域名应继续允许 Agent 上报和 Dashboard 使用现有认证；**不要**在原域名安装强制 Cloudflare Access 策略。
- MiniProbe 每个私有请求校验 Access JWT 的公钥签名、Team issuer、AUD、唯一邮箱、时间及所访问 hostname。公钥缓存 10 分钟，密钥轮换与无法取证时按拒绝访问处理。
- 想停用：SSH 输入 `miniprobe → 15 → 11 → 1 → 2` 输入 `DISABLE`，撤销 MiniProbe Access 自动登录；如需删除 Cloudflare 侧规则，另去 Cloudflare Dashboard 操作。注意停用本地认证**不会**自动删除 Cloudflare Access 应用/DNS/Tunnel 路由。
- 云端 OTP 收不到邮件：检查 Access Allow Policy 与 OTP 登录方式、邮箱垃圾邮件以及 Cloudflare 服务状态。任何时候仍可通过 SSH `miniprobe → 15 → 6` 修改原订阅独立密码。
- Access 与 Tunnel 均为两套不同设置。只有 Tunnel 不代表 Access 已配置；不建议尝试把 Access 直接安装在原 Agent 所在域名。
- 密码、Token、密钥与私人地址均不得写入仓库。
