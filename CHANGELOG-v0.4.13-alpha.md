# MiniProbe v0.4.13-alpha

## New

- 线路详情新增可读骨干网识别，不再要求用户只靠 ASN 数字判断线路。
- 中国电信：`AS4809 -> CN2`，`AS4134 -> 163 / ChinaNet`，并可识别 `AS23764 -> CTGNet`。
- 中国联通：`AS9929 -> CUII / 9929`，`AS4837 -> 4837 / China169`，并可识别 `AS10099 -> CUG`。
- 中国移动：`AS58807 -> CMIN2`，`AS58453 -> CMI`，`AS9808 -> CMNET`。
- 当前线路与基准线路使用同一套识别规则；发生路由事件时同时显示例如 `CN2 -> 163 / ChinaNet` 的可读变化，并继续保留完整 ASN Path。
- 识别规则采用明确 ASN 命中与优先级，不根据运营商名称猜测；无法识别时显示“其他 / 未识别”，不声称 CN2 GIA 等仅凭 ASN Path 无法可靠确认的产品等级。

## Versioning

- 本版只修改 Server / Dashboard 展示，不改变 Agent traceroute、RTT、流量或上报协议。
- Server 版本提升到 `v0.4.13-alpha`，当前目标 Agent 版本保持 `v0.4.12-alpha`，菜单 12 不会要求现有 v0.4.12-alpha Agent 做无意义的重复升级。
- 从本版起 Server 与目标 Agent 版本在代码中显式解耦，后续纯 Server/UI 修复可独立发布。

## Compatibility

- 完整兼容 v0.4.12-alpha 已采集的 ASN 路由状态和历史文件，无需重建线路基准。
- 主 Dashboard 密度、31 天线路历史、256 MiB 独立上限、路由变化连续两次确认及原始 hop IP 不落盘规则均保持不变。
