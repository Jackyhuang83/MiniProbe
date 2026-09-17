# MiniProbe v0.4.2-alpha

Dashboard and domestic line-test refinement based on the original compact VPS-probe reference layout and real iPhone testing of v0.4.1-alpha.

## Domestic line test

- Added a Server-global SSH menu: `11. 国内线路测试`.
- Test city can be switched between Beijing, Shanghai and Guangzhou.
- Test protocol can be switched between ICMP, TCP and UDP; probing can also be disabled.
- The selected settings are delivered to Agents through a separate Ed25519-signed Probe Policy, keeping the existing signed traffic/endpoint policy backward-compatible with v0.4.1 Agents.
- Dashboard carrier labels include the city, for example `广州电信`, `广州联通`, `广州移动`.
- The active protocol is shown as a small low-emphasis tag in the upper-right of the three-carrier area.
- ICMP measures Echo RTT and packet loss.
- TCP measures TCP/53 connect RTT and connect failure rate.
- UDP measures UDP/53 DNS-query RTT and query failure rate.
- Default for upgraded installations: Guangzhou + ICMP.

## Probe targets

- Beijing: Telecom `219.141.136.10`, Unicom `202.106.0.20`, Mobile `221.130.33.60`.
- Shanghai: Telecom `202.96.209.133`, Unicom `210.22.70.3`, Mobile `211.136.112.50`.
- Guangzhou: Telecom `202.96.128.86`, Unicom `210.21.4.130`, Mobile `211.136.192.6`.

These are regional ISP DNS targets used only as fixed personal-monitoring reference points; protocol-specific filtering may affect TCP/UDP results.

- Current city presets are IPv4 reference targets. IPv6-only Agents without IPv4 egress report these probes as N/A; other monitoring remains unaffected.

## Dashboard

- Reworked the three-carrier area to match the compact reference structure.
- Removed the oversized `线路质量` title and full-width single history bar.
- Each carrier uses two compact side-by-side tracks: latency history on the left and loss/failure history on the right.
- Shows the latest 20 samples per track for a denser mobile layout.
- Latency and loss/failure use independent color scales; timeout / 100% failure is red.
- Existing old Agent reports remain readable through a compatibility fallback.

## Agent

- Added independent latency-history and loss/failure-history samples while keeping the previous combined history field for compatibility.
- Added native ICMP echo probing and UDP DNS probing without external commands or third-party services.
- Probe history resets logically when city/protocol changes, so samples from different test modes are not mixed.

## Installer

- Server upgrades download to a temporary file and atomically replace the running binary, avoiding `curl: (23) Failure writing output to destination`.
- Agent installer uses the same atomic replacement pattern, preserves `/var/lib/miniprobe-agent/state.json`, and now explicitly restarts an already-running systemd Agent so upgrades take effect immediately.

## Compatibility

- Database schema version is now 5; v0.4.1 data migrates automatically and keeps nodes, tokens, Dashboard password, traffic state and Telegram configuration.
- v0.4.1 Agents continue reporting to a v0.4.2 Server because the new Probe Policy is a separate signed response field ignored by older Agents.
- Upgrade Agents to v0.4.2 to use selectable city/protocol probing and the new independent history tracks.
