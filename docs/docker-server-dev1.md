# MiniProbe Docker DEV1 — 与 HomeSphere 隔离部署

状态：开发中，仅用于 PR 审查及 CI 验证；不代表已部署。当前 Server v0.4.30-alpha，Agent 目标版本仍为 v0.4.28-alpha。

## Docker 菜单 8：Tunnel 状态和安全操作（v0.4.33-alpha）

在 VPS 宿主机执行 `sh /opt/miniprobe-docker/manage.sh menu`，选择 `8`，提供：

- `1`：仅读取 Server 容器健康状态、Cloudflare Tunnel 容器状态，以及专用 Token 文件是否存在；不输出 Token 内容
- `2`：只有 Server healthy、Token 文件属主为 UID 10001 且权限为 0600 时才允许继续；宿主机确认输入 `START`，启动独立 Tunnel；已运行则不重启
- `3`：提示 HTTPS Dashboard 与 Agent 上报可能断开；宿主机输入 `STOP` 才停止独立 Tunnel；不会自动开放公网 HTTP
- `0`：返回，不修改配置

容器只通过固定退出状态码（74/75/76）向宿主机管理脚本请求动作；不共享 Docker Socket，也不提供公网管理 API。运行状态**不代表** Cloudflare Tunnel 已认证成功或公网链路畅通，仍需从公网检查 HTTPS 和节点上线状态。直接在容器内运行二进制不会执行宿主机管理动作。

宿主机另可执行 `sh /opt/miniprobe-docker/manage.sh tunnel-status` 仅读状态。首次启用 Tunnel 前，仍须在 Cloudflare 控制台配置 MiniProbe 专用 Tunnel/域名、将 Public Hostname Service 指向 `http://127.0.0.1:28888`，并由管理员在宿主机安全保存独立 Token。不会复用 HomeSphere 凭据。

**部署注意**：容器镜像与宿主机 `manage.sh` 都必须更新到本功能对应版本，才能从菜单 8 调用宿主机。不能只升级容器镜像。

## 目标和隔离设计

- MiniProbe Server 单独使用 Compose 项目名 miniprobe，不与 HomeSphere 共享容器、网络、数据库、Docker Socket、Tunnel Token 或文件挂载。
- Server 运行 UID/GID 10001，根文件系统只读、capabilities 全部移除、no-new-privileges。持久数据只挂载 ./data 到 /var/lib/miniprobe。
- 宿主机只监听 127.0.0.1:28888；没有公网管理端口，也不支持 WebShell 或网页写入。
- 可选 cloudflared 是另一个独立容器，但它共享 MiniProbe Server 的网络命名空间，以继续使用原有 Cloudflare 入口 http://127.0.0.1:28888。保留只信任 loopback 的 X-Forwarded-Proto 安全判断，不能改成 Docker 桥接来源直接被信任。
- 512MiB、0.75 CPU 是 Server 初始限制；cloudflared 再用 128MiB、0.25 CPU。日志单独限额。是否足以共存必须以 HomeSphere 高峰期实测为准。
- 原管理入口在宿主机 SSH 执行 sh manage.sh menu。容器模式屏蔽菜单 8 对 systemd/Tunnel 的控制。菜单 13 使用固定退出码通知宿主机管理脚本检查 GHCR 新镜像，宿主机负责交互确认、摘要固定、健康检查与故障回滚；不在容器内替换二进制。也可以直接 sh manage.sh update；指定镜像仍使用 sh manage.sh upgrade。
- 所有容器只在本机操作，无宿主机特权容器。
- 镜像包含三种架构 Agent 下载文件，维持原有远端 Agent 下载安装和升级功能。

## 开发阶段的文件

- deploy/docker/Dockerfile：Go 1.26.10 多阶段构建，linux/amd64 和 linux/arm64。
- deploy/docker/compose.yaml：独立 Server 服务。
- deploy/docker/compose.tunnel.yaml：可选 MiniProbe 专用 Cloudflare 连接器。
- deploy/docker/manage.sh：中文管理与镜像回滚入口。
- deploy/docker/entrypoint.sh：防止迁移时数据库缺失而生成错误的新 Server 签名身份。
- .github/workflows/validate-miniprobe-docker.yml：PR 容器烟测，不推送镜像。
- .github/workflows/docker-miniprobe.yml：合并或手动触发时向 GHCR 发布多架构镜像。

GHCR 仅在人工触发发布后提供开发标签 ghcr.io/jackyhuang83/miniprobe-server:docker-dev1，以及固定提交标签 ghcr.io/jackyhuang83/miniprobe-server:sha-<commit>。发布前尚不存在对应的镜像；不能在 CI 通过前认定可用。正式迁移建议将 .env 中 MINIPROBE_IMAGE 固定为已验证的 sha 标签或 digest，避免 latest 意外更新。GHCR 包可能默认是 Private，需检查访问权限；不得向聊天或 GitHub 仓库上传令牌。

## 迁移前读取新 VPS 环境

在运行 HomeSphere 的新 VPS（SSH）检查 CPU、内存、磁盘、Docker 容器资源与 28888 端口：
- free -h
- df -h /
- docker stats --no-stream
- ss -lnt | grep ':28888 '
- docker compose version

这些命令只读，不会干扰 HomeSphere。

## 迁移流程（待用户确认后才实际操作）

1. 旧 VPS 停止 MiniProbe Server 写入，保留可恢复的完整备份。停止服务之后再制作最终一致性快照。
2. 加密/受保护通道传送整个 /var/lib/miniprobe/ 到新 VPS 独立目录下的 data/。必须包括 miniprobe.json、subscriptions.json、network-history/、traffic-history/ 及 subscription-backups/。miniprobe.json 内含签名密钥和 Agent 身份信息，不能遗漏，也不能公开。
3. 在新 VPS 独立目录存放 compose.yaml、compose.tunnel.yaml、manage.sh、.env；不得复制 HomeSphere 的 Compose 文件。复制 .env.example 并设置经验证的镜像引用，不在 .env 写任何 Token。新 VPS data/ 必须由 UID 10001 持有，可在宿主机 root 下执行 chown -R 10001:10001 data；data/ 0700，敏感 JSON 0600。
4. 在新 VPS 独立目录运行 sh manage.sh start，再运行 sh manage.sh menu 核对节点和订阅中心。这一步不启用新公网 Tunnel，且旧 Server 不得同时运行。
5. 如果原 MiniProbe 使用 Cloudflare Remotely-managed Tunnel，保留现有访问域名和远端 Hostname Service http://127.0.0.1:28888；仅将旧 MiniProbe 专用 Tunnel Token 安全转移到新 VPS 的 secrets/cloudflared.token 中。其所在 secrets/ 目录权限设为 0700，Token 文件属主 UID/GID 10001:10001、权限 0600，便于非 root cloudflared 读取。可在宿主机 root 下执行 chown 10001:10001 secrets/cloudflared.token 和 chmod 0600 secrets/cloudflared.token。确认已停止旧 VPS 的 MiniProbe Tunnel 后，在新 VPS 执行 sh manage.sh tunnel-start。绝不使用 HomeSphere 的 Tunnel Token。
6. 检查公网 HTTPS /healthz、节点在线、流量历史、订阅独立 HTTPS 登录和 Telegram。HomeSphere 原有直播、Emby、115 功能不得受到影响。
7. 失败时先停止新 Tunnel 和 Server，再恢复旧 VPS 服务。若新 Server 已写入较新数据，恢复前应保存并核对其变化，不能用旧快照覆盖后声称无数据损失。不要立刻销毁旧 VPS。

若旧 Server 的 Agent 地址为公网旧 IP 而非原 HTTPS 域名，上述无感切换不适用：必须先规划 Agent 端点迁移。Docker 部署不擅自修改 Agent Token、Server 密钥或网络分流。

## 管理命令（新 VPS 的 MiniProbe Docker 独立目录）

- sh manage.sh start：检查旧数据存在并启动 Server。
- sh manage.sh status：查看容器。
- sh manage.sh menu：进入原 SSH 菜单；选择 13 时由宿主机接管镜像检查与安全升级（需要交互式 SSH）。
- sh manage.sh update：直接检查最新已发布 Docker 镜像，相同镜像不重启，有更新时询问 YES。
- sh manage.sh logs：查看最近 100 行日志。
- sh manage.sh stop：停止 Server。
- sh manage.sh tunnel-start / tunnel-stop：管理 MiniProbe 专用 Tunnel，不动 HomeSphere。
- sh manage.sh upgrade ghcr.io/jackyhuang83/miniprobe-server:sha-<commit>：先拉取镜像，后应用并检查健康；异常回滚旧镜像并重建 Tunnel 网络命名空间。

## 已知限制

- 默认将管理 Unix Socket 放在容器内部，不向宿主机或 Web 暴露。若 HomeSphere VPS 本身也运行 MiniProbe Agent，现有 Agent 对本机 Server 的 socket 检测可能不会触发；它仍可通过原有 HTTPS 域名连接，需专项验证同机回环行为。
- 容器烟测不能证明 Cloudflare 实际公网切换成功；原 Tunnel Token 及 HomeSphere 高峰期资源占用都只能在用户授权的 VPS 上实机检查。
- 镜像回滚不是数据库格式降级工具。格式不兼容的未来版本要先独立备份。
- Docker 只是进程、网络和磁盘挂载隔离，两项目仍会共享宿主机 CPU、内存、网络及实际硬盘空间。

## Docker 订阅中心首次设置

Docker 安装不需要另行安装宿主机版 MiniProbe。在 VPS SSH 中进入 MiniProbe Docker 部署目录，运行 `sh manage.sh menu`；管理脚本通过 `docker compose exec server` 打开**容器内**的管理菜单。选择 `15 → 6` 设置独立订阅密码（至少 12 个字符），随后通过现有 HTTPS 域名登录订阅中心。

密码由容器内 Server 经本地 Unix 管理 Socket 写入 `/var/lib/miniprobe/subscriptions.json`，实际保存为 Compose 挂载的 `./data/subscriptions.json`。该文件仅保存密码哈希，权限为 `0600`。镜像升级或容器重建必须保留 `./data`。无需暴露 Docker Socket 或公网管理接口。

网页显示“尚未设置独立订阅密码”说明 HTTPS 安全判定已通过、但密码尚未初始化；HTTPS 判定失败会显示单独的错误提示，不应该为了设置密码而放宽 HTTPS 的安全检查。
