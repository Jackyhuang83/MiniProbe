# MiniProbe v0.4.23-alpha

## Optional IP attribute detail (Server-only)

- Add **14. IP 属性检测（默认关闭）** to SSH-only local management. Must explicitly input YES after consent notice explaining that the external provider sees monitored node egress IPs. Dashboard remains read-only.
- Add small per-node default-collapsed IP attributes section, disabled by default; IPv4 and IPv6 shown separately.
- On opt-in, Server queries ipquery.io over HTTPS at **most once per node per address family per UTC calendar day**, counting failed attempts; persist attempt day before external network call to avoid reboot-induced extra calls. Serialize and rate-space calls; fail closed.
- Provider signals: IDC (datacenter), mobile, non-IDC (home connection NOT verified), unknown; country code and known VPN/proxy/Tor flags. Native/non-native remains **unknown** because evidence is insufficient.
- Existing Agent IPv4/IPv6 egress discovery is reused without storing raw outbound IP in the new module. Only classification, country and timestamps persist. No token or private user information is added to GitHub.
- Agent target remains **0.4.22-alpha**; a Server update suffices for this optional feature. The next Agent egress IP report may take up to 12 hours.
- Testing includes default-off, daily UTC cap across restarts, IPv4 vs IPv6 rate limits, conservative classification and malformed response rejection.
