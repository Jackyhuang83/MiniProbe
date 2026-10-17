# MiniProbe v0.4.21-alpha

## Server-only update experience and reliability

- Add local **13. Server 检查更新 / 升级** to the SSH-only `miniprobe` menu.
- Compare current Server with latest published official GitHub Release; request explicit YES before installation.
- Restrict downloads to fixed official release binaries, verify SHA256 for Server and Agent assets, preserve old binaries for rollback, replace atomically and restart.
- Verify the **running** new Server version over the local-only admin socket, and attempt rollback if restart/validation fails. Menu exits after a successful update to avoid stale version display.
- Harden `scripts/install-server.sh` with checksums *before* replacement and fail-closed running-version verification instead of silently ignoring health checks.
- Preserve all node records, traffic counters, tokens, Dashboard password and network settings.
- Agent target remains **0.4.20-alpha** (Server-only update).
- One-time bootstrap from older versions still requires official installer; subsequent Server updates use menu 13.
