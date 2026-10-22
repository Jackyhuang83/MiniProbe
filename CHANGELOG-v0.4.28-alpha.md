# MiniProbe v0.4.28-alpha

## 安全更新：Server / Agent 统一使用 Go 1.26.10 构建

- **Go 编译工具链从 1.23 升级至 1.26.10**，采用 2026-10-19 官方安全修复，涵盖 `crypto/x509` 证书链验证相关的 CVE-2026-111943。
- 重新构建 **Server (linux/amd64、linux/arm64)** 与 **Agent (linux/amd64、linux/arm64、linux/armv7)**，使补丁真正进入 Go 静态编译二进制，而不只是升级开发环境。
- Server 版本：`0.4.28-alpha`；Agent 新版：`0.4.28-alpha`，用于准确区分已修补和旧版 Agent。
- GitHub Actions 使用固定 `go 1.26.10`，禁止作业自动切换任意 Go 工具链，并在测试、跨架构构建后使用 `go version -m` 检查发布二进制的嵌入编译器版本。
- 将活跃 CI 构建动作更新为 `actions/checkout@v5` / `actions/setup-go@v6`，GitHub 托管环境中不再依赖这两个 Action 的 Node 20 运行时。
- 维持 `CGO_ENABLED=0`、静态二进制、现有 Go 标准库实现和无外部 Go 模块依赖。

### 安装和兼容

- 本版本无业务功能变动，不修改数据库、流量历史、额度、订阅到期日、代理、Telegram、数据保护和配置文件。
- **先升级 Server**，验证服务与 Web 功能正常。生产 VPS **不需要安装 Go**。
- **Agent 不自动升级**。已安装旧 Agent 仍可继续连接，之后可在 Server 的 **菜单 4 Agent 管理** 查看新目标版本、按原来的受控自更新流程由管理员输入 `YES` 后分批升级并观察。
- GitHub Release 仅在所有测试成功后才生成，发布不会直接变更 VPS。无法保留旧编译器编译产物中的潜在补丁缺失，因此想完成整个监控网络的修复需要后续升级各 Agent。
