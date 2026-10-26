# MiniProbe v0.4.42 — VPS 备注标签编码与显示修复

基于 v0.4.41，优化 MiniProbe Server 的 VPS 节点备注标签输入与网页显示，不改变 Agent 行为。

## 修复
- 通过 Server SSH 的 miniprobe 菜单添加/修改 VPS 时，备注标签支持 UTF-8 中文、繁体字和 emoji。
- 对替换字符 U+FFFD、非法 UTF-8、控制字符、过长标签进行明确提示并拒绝保存，避免原始文本静默变成乱码；支持最多 8 个标签，每项不超过 30 个 Unicode 字符。
- Dashboard 标签提高可读性，长文本自动换行，保留 HTML 转义防止输入内容成为网页脚本。
- 遇到已经保存为替换字符的旧备注，网页提示「备注编码异常，请重新填写」。损坏前的字节无法恢复，用户需在 SSH 修改节点时重新输入原文。
- 包含 Go 与 JavaScript 的中文、emoji、畸形编码、标签长度及 HTML 安全回归测试。

## 升级
- MiniProbe Server 更新至 v0.4.42：Server 宿主机 SSH 执行 miniprobe，选择菜单 13。
- Agent 目标版本仍为 v0.4.39，无需更新 Agent，不会自动更改正在运行的 VPS。
- 保留 v0.4.41 的 VPS 月付 / 年付功能和 v0.4.40 订阅时区、币种优化。
