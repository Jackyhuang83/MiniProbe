# MiniProbe v0.4.24-alpha

## Server update menu hotfix

- Fix `miniprobe` SSH menu 13 error: `检查更新失败: 更新元数据超过大小限制`.
- GitHub `releases?per_page=20` returns ~263 KiB in this repository, exceeding the original 128 KiB bound. Fetch 5 newest published releases and use a still-hard 1 MiB JSON metadata bound.
- Retain draft/tag validation, HTTPS release asset download and SHA256 check, local Unix socket verification and rollback logic.
- Regression test >128 KiB valid metadata parsing and >1 MiB rejection.
- **Server-only** update; Agent target stays v0.4.22-alpha. No changes to node data, IP attributes opt-in privacy or Cloudflare mode.
- Affected v0.4.22/23 servers need to run the official version-pinned Server installer once. Future updates use menu 13 again.
