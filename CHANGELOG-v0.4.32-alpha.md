# MiniProbe v0.4.32-alpha — Docker 菜单 13 升级修复

- Docker Server SSH 菜单 13 不再只输出禁止更新提示；通过退出码 73 通知宿主机 `manage.sh menu` 安全处理。
- 宿主机从 MiniProbe GHCR 拉取当前已发布的 Docker 镜像，并解析、验证官方仓库的不可变 SHA256 摘要；与当前镜像相同则不重启。
- 有更新时仅在交互式 SSH 终端输入 YES 才执行原有的镜像升级、健康检查及失败回滚流程。
- 增加 `sh /opt/miniprobe-docker/manage.sh update` 入口，继续保留 `upgrade <sha-tag/digest>`。
- 不挂载 Docker Socket，不提供公网写入接口，不修改 Agent、Server 数据、Cloudflare Tunnel Token 或 HomeSphere。
- 单元测试和 Docker CI 烟测覆盖菜单 13 退出码与容器安全边界。

**部署提醒**：使用宿主机 `sh /opt/miniprobe-docker/manage.sh menu`（或指向该脚本的 `miniprobe` 命令），而非直接在容器内调用二进制。宿主机部署脚本本身也需更新后才有新功能。
