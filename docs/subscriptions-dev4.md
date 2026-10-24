# MiniProbe 订阅中心 DEV4 — HTTPS 私有网页管理

## 可用操作
- HTTPS 登录订阅中心，点击「添加订阅」可填写名称、分类、币种、金额、付款周期、到期日期、状态、自动续费标记、标签及备注。
- 手动订阅卡片支持「编辑」「记录续费」「删除」。删除要求浏览器二次确认；历史续费流水不删除。
- VPS 自动关联卡片只支持「记录付款」，不提供编辑或删除；实际节点费用、日期仍在 MiniProbe 管理菜单「修改节点」中维护。
- 「记录续费」登记真实日期和金额，可选输入新到期日期（仅手动且非长期订阅）；历史仅追加，不允许修改已发生的付款记录。

## 身份与安全
- 独立订阅密码仍在 Docker 容器内管理菜单 `15 → 6` 设置；无密码时网页不开放。
- **只支持 HTTPS**，沿用严格 loopback cloudflared 的 `X-Forwarded-Proto` 信任范围，不信任公网伪造代理标头。
- POST/PUT/DELETE 写入前必须有效且未失效的订阅专属 HttpOnly、Secure、SameSite=Strict Cookie。新 CSRF 令牌仅通过已认证 HTTPS GET `/api/v1/subscriptions/session` 返回，绑定登录 Cookie；客户端通过 `X-MiniProbe-Subscription-CSRF` 回传。
- CSRF 外再校验严格 HTTPS Origin 与 Host 同源、`X-MiniProbe-Request: 1`、JSON Content-Type；跨站请求和仅持 Dashboard Cookie 的请求均拒绝。
- 公共写入面仅为 `/api/v1/subscriptions/manage`（POST/PUT/DELETE）和 `/api/v1/subscriptions/renewals`（POST）。不新增管理密码、备份/恢复、节点编辑、Telegram 写入的公网 API。
- 复用现有独立订阅账本 `/var/lib/miniprobe/subscriptions.json`，仍由 Docker `./data` 数据卷持久化。写入文件权限 `0600`，不改变格式版本。
- UI 使用 `textContent` 呈现用户记录，禁止存放第三方登录密码、API Token 或支付卡号。

## 测试
- `go test ./...`：校验无会话 / Dashboard 会话 / HTTP / 错误 Origin / 缺失或错误 CSRF / 非 JSON 拒绝写入，合法 CRUD 与续费持久化、VPS 防改写及密码轮换撤销。
- `node scripts/test-subscription-dev4.cjs`：浏览器逻辑模拟添加、修改、续费、删除及 CSRF 请求头，检查 VPS 无编辑删除按钮。
- CI 包含所有 Go 测试、JS 语法和浏览器回归，Docker 构建以发布流程为准，不自动触碰 VPS。

## 升级
CI 通过、PR 合并并确认 GHCR 镜像发布成功后，SSH 执行 `sh /opt/miniprobe-docker/manage.sh menu`，进入 `13` 检查升级。升级保留 `./data` 数据卷和 Tunnel 配置。无需修改 Agent。
