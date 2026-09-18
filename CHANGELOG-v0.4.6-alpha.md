# MiniProbe v0.4.6-alpha

Correctness and network-identity release based on real Dashboard observation of v0.4.5-alpha.

## Rolling packet-loss / failure rate

- Fixed the Dashboard percentage semantics: `loss_pct` no longer represents only the newest four probe attempts.
- Each carrier now keeps the same 20-round window that is shown by the loss/failure history track.
- ICMP uses 4 Echo attempts per round, so a full window represents up to 80 packets; TCP/UDP use the same rolling-attempt model for connect/query failure rate.
- The displayed percentage is calculated from exact sent/lost counts, not reconstructed from color buckets.
- The per-round history blocks remain independent, so short spikes are still visible while the percentage reflects the whole visible window.
- Added regression coverage proving the oldest lossy round leaves the percentage when it leaves the 20-round display window.

## IPv4 / IPv6 network nature labels

- Keeps the v0.4.5 base labels: `V4`, `V4 NAT`, `V6`.
- Adds best-effort family-specific labels such as `V4 家宽`, `V4 广播`, `V6 原生`, and `V4 NAT 家宽`.
- Public IPv4 and IPv6 are discovered separately; the public addresses themselves are not exposed through the Dashboard.
- Network risk/location intelligence plus RDAP registration country are used conservatively:
  - `家宽` only when the network is not classified as datacenter/mobile/VPN/Tor/proxy and the ISP/organization matches a known fixed-access ISP pattern.
  - `原生` when geolocation country and RDAP registration country match.
  - `广播` when those two country codes differ.
- If evidence is incomplete or lookups fail, MiniProbe leaves the base V4/V6 label unchanged instead of guessing.
- Attribute refresh runs at Agent startup and then once every 24 hours; failures do not affect normal monitoring/reporting.

## Version

- Agent version updated to `0.4.6-alpha`.
- Server installer default release updated to `v0.4.6-alpha`.
