# MiniProbe v0.4.36-alpha — DEV4 订阅中心显示错误热修复

- 修复 DEV4「我的订阅」栏遗漏 `subShown` 元素，导致登录后 JavaScript 报 `null is not an object`、订阅列表未能渲染。
- 恢复订阅计数并与「＋ 添加订阅」按钮共同布局，适配移动端窄屏。
- 将 DEV4 浏览器模拟测试改为检查真实 HTML 元素 ID：检测缺失、重复的 ID，模拟 DOM 不再自动创建缺失元素，阻止同类回归。
- 统一升级 Server 与资源路径版本至 `v0.4.36-alpha`，避免缓存与旧资源混用；合并后由 Makefile 版本变化触发 Docker GHCR 多架构镜像构建。
- 不修改订阅账本文件、数据结构、独立密码、节点配置、Agent、Tunnel 或 HomeSphere；无需迁移数据。

部署：镜像发布成功后经现有 Docker 菜单 13 安全更新，刷新 HTTPS 订阅网页，核对「显示 N / M」与添加订阅操作。
