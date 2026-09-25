# MiniProbe 跨对话连续性与隐私规则

> 目的：保证 MiniProbe 在切换新对话后能够准确接续项目，同时避免在项目交接、记忆或状态文档中保存用户隐私与敏感信息。

## 1. 文档职责分离

### `PROJECT_STATE.md`
只记录“当前做到哪里”：
- 当前版本 / 基线；
- 当前正在处理的 Bug 或明确需求；
- 最新有效技术测试结果；
- 已确认技术结论；
- 明确的下一步。

### `MiniProbe-SKILL.md`
只记录长期稳定规则：
- 架构边界；
- 安全原则；
- 平台兼容性；
- 发布 / 测试流程；
- 已验证且长期有效的设计约束。

### `CHANGELOG`
只记录历史版本变化，不承担跨对话当前状态交接。

## 2. 新对话启动规则

当用户说“继续 MiniProbe”、直接贴日志、测试结果或继续上一个 MiniProbe 任务时，处理顺序必须是：

```text
读取 PROJECT_STATE.md
-> 读取 MiniProbe-SKILL.md 相关章节
-> 核对项目名 / 当前版本 / 当前问题
-> 再分析用户的新输入
```

禁止仅凭 `VPS`、`IPv4`、`IPv6`、`脚本`、`网络测试` 等关键词，把 MiniProbe 误路由到 `vps-bootstrap` 或其他项目。

## 3. 对话交接规则

当当前 MiniProbe 对话接近长度上限时，必须先更新 `PROJECT_STATE.md`，再建议新建对话。

交接内容只包括：

```text
当前版本
本轮已完成事项
最新有效测试
尚未解决的问题
已确认结论
下一步
```

不得要求用户在新对话重新复述已写入状态文件的项目进度。

## 4. 隐私与敏感信息原则

`PROJECT_STATE.md`、`MiniProbe-SKILL.md`、交接摘要及类似项目连续性文档，只允许保存完成 MiniProbe 技术工作所必需的信息。

### 严禁保存

- 密码；
- Token / API Key；
- 私钥 / SSH Key；
- Cloudflare Tunnel Token；
- Cookie / Session；
- 私人邮箱；
- 手机号；
- 家庭住址；
- 精确位置 / 经纬度；
- 身份证件号；
- 银行卡 / 支付信息；
- 私人账号唯一标识；
- 与 MiniProbe 无关的个人生活资料；
- 用户没有明确要求长期保留的敏感信息。

## 5. 日志与排障信息脱敏规则

如果用户提供的终端输出、日志、配置文件中包含敏感内容：

```text
原始信息只用于当前排障
-> 写入项目状态前进行脱敏
-> 用占位符替代敏感值
-> 仅保留技术判断所需内容
```

推荐占位符：

```text
<TOKEN_REDACTED>
<API_KEY_REDACTED>
<PASSWORD_REDACTED>
<PRIVATE_IP_OR_HOST_REDACTED>
<EMAIL_REDACTED>
```

不得为了“方便以后排障”而把真实敏感值写入交接文档。

## 6. 项目隔离规则

MiniProbe 与 `vps-bootstrap` 等其他项目即使共享以下技术词汇：

```text
VPS
IPv4 / IPv6
DNS
Cloudflare
网络测试
脚本
```

也不得因此混用上下文。

项目连续性判断优先级：

```text
PROJECT_STATE.md
> MiniProbe-SKILL.md
> 当前对话明确上下文
> 一般历史记忆 / 关键词相似度
```

## 7. 最小化保存原则

所有项目状态文档遵循：

> 能不保存就不保存；能抽象就不记录具体值；能脱敏就不保留原文。

只保存未来继续技术工作真正需要的信息。

## 8. 当前 MiniProbe 交接文件

当前跨对话接棒文件：

```text
PROJECT_STATE.md
```

其内容必须持续遵守本规则。
