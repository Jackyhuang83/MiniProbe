# MiniProbe v0.4.5-alpha

Small usability release focused on node identity and safe incremental editing. Monitoring architecture and Dashboard card hierarchy are unchanged.

## Network type badges

- Agent now classifies the node's configured network address families and reports compact network labels to the Dashboard.
- Public IPv4 is shown as `V4`.
- Private / CGNAT IPv4 without a public IPv4 interface is shown as `V4 NAT`.
- Global IPv6 is shown as `V6`.
- Dual-stack combinations are supported, including `V4 · V6` and `V4 NAT · V6`.
- Link-local and loopback addresses are ignored so they do not create false badges.
- The labels appear on the existing OS / virtualization / architecture row; no new Dashboard panel or card height is introduced.

## Safer node information editing

- SSH menu `3. 修改节点 / 流量策略` now uses partial updates.
- Pressing Enter leaves that field exactly unchanged instead of rebuilding the whole node configuration from prompt defaults.
- This makes it safe to add only one missing item, such as a monthly traffic quota, while leaving price, expiry date, tags and other fields untouched.
- Optional fields can be explicitly cleared with `-` where shown.
- Invalid values abort the edit before anything is saved.
- Added regression tests proving that a traffic-only update preserves all unspecified node fields.

## Version

- Agent version updated to `0.4.5-alpha`.
- Server installer default release updated to `v0.4.5-alpha`.
