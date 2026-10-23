# MiniProbe v0.4.33-alpha — Docker 菜单 8 Tunnel 管理优化

- SSH 菜单 8 在 Docker 部署时改为：查看 Server/Tunnel 本机状态、启动、停止、返回。
- 通过固定退出码 74/75/76 交接给宿主机 `manage.sh menu`，不授予容器 Docker Socket/systemd/任意宿主机命令能力。
- 宿主机启动前校验 Server healthy、MiniProbe 独立 Token 属主 UID 10001/权限 0600；输入 START 才执行，已运行则不重启。
- 停止前显示 Agent 和 Dashboard 可能中断的风险，输入 STOP 才执行；不自动放开 Direct HTTP。
- 状态显示仅包括本地 Server 健康、Tunnel 容器与 Token 文件存在性，不读取或泄漏 Token，不冒充端到端公网健康。
- 新增 `manage.sh tunnel-status`，保留既有 tunnel-start/tunnel-stop 命令。
- Go 单元测试、Docker 烟测和静态隔离检查涵盖菜单 8 的固定退出码和安全边界。
- 仅 Server、Docker 管理脚本与 UI 资源版本变化；Agent 功能与版本不变。

**部署步骤**：CI 全绿并合并发布后，先在宿主机更新 `manage.sh`，再通过原 Docker 安全升级流程更新镜像。两者都更新后菜单 8 才能完整工作。保留独立备份与 Cloudflare Token，不直接修改生产 VPS。
