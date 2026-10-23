# MiniProbe 订阅管理中心 · DEV1

> 独立开发分支，跟踪 Issue #21。仅修改 Server；不会修改 Agent、监听端口或网络拓扑 PR。

## 如何测试（仅在 DEV1 PR 通过 CI、人工批准合并、升级 Server 后操作）

1. 在安装 MiniProbe Server 的 VPS 上通过 SSH 执行 `miniprobe`，选择 **15. 订阅管理中心**。
2. 首次使用先选 **6. 设置/更换私有网页访问密码**。必须设置至少 12 字符的**独立密码**，不可直接复用 Dashboard 登录口令。
3. 返回选 **1. 查看全部订阅**：现有 VPS 节点自动出现在列表中，费用与到期日期取自 MiniProbe 节点配置，不必再次录入。
4. 选 **2. 新增订阅**，录入影视、AI、域名、软件等普通订阅。回到 **1** 核实；选 **3 / 4 / 5** 测试编辑、状态、删除（删除必须输入 `DELETE`）。
5. 使用 **HTTPS** 打开 `https://<你的 MiniProbe 域名>/ui/0.4.28-alpha/subscriptions.html`，以第 2 步的独立密码登录。页面只有浏览和筛选，不支持网页增删改。
6. 如果 Dashboard 处于 `public`，使用无痕浏览窗口直接请求 `/api/v1/subscriptions`，应返回 **401**，不能展示任何订阅数据。Dashboard 密码及 Cookie 均不能替代订阅独立会话。
7. 到 SSH 节点菜单改一次 VPS 月租或到期日，刷新订阅页面，应立即显示更新。订阅中心不写回原 VPS 节点。
8. 重新设置独立密码后，旧浏览器应失去访问权限。

## 实施边界

- 私有文件：与 `miniprobe.json` 同目录的 `subscriptions.json`，权限 0600，原子写入，1 MiB 上限 / 128 条手动订阅。VPS 不另存副本，因此即使旧 Server 回滚也不会在它的数据库保存流程中丢掉新订阅记录。
- 只有本机 root-only Unix Socket 提供订阅创建、修改、删除及独立密码设置。公网 HTTP 接口只有独立认证后的只读订阅查询。
- 公开监控模式 public/protected/disabled 对订阅认证**无影响**。订阅登录与详情仅在 HTTPS（或来自本机可信反向代理的 HTTPS 请求）提供。
- 独立 Cookie 使用 HttpOnly、Secure、SameSite=Strict、24 小时有效期。改密码使所有旧订阅会话失效。
- 金额采用整数最小币种单位，不把 JPY 等零小数币种错误按美分显示；不擅自跨币种换算。
- 不存储信用卡、订阅账户密码、Cookie、Token；备注字段也不应填写这些敏感内容。
- DEV2 的 Telegram 到期通知、手动续费历史及 DEV3 的统计、汇率、日历、备份**尚未实现**。

## 开发验证

本 PR 的校验流程运行 `go test ./...`、`go vet ./...`、`node --check cmd/miniprobe-server/web/subscriptions.js` 和跨平台编译（`make release`，不发布）。

手动核对：
- Public Dashboard 下匿名访问订阅 API = 401。
- 替换 Dashboard 登录 Cookie 不构成订阅认证。
- 非 HTTPS 登录不成功。
- VPS 不创建第二份账本记录，且菜单不能修改/删除 VPS 镜像。
- 修改/删除时错误输入不能破坏旧数据。
