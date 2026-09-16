# MiniProbe v0.4.1-alpha

Dashboard refinement release based on the first real VPS test of v0.4.0-alpha and the original compact VPS-probe reference layout.

## Dashboard

- Switched the Dashboard to a compact dark card layout with clearer hierarchy between node identity, resource metrics, traffic/plan summary and line-quality probes.
- Added country/region flag before node names when the node name or tag contains a common two-letter region code such as HK, JP, SG or US.
- Added local distro identity artwork for Debian, Ubuntu, Alpine Linux, Rocky Linux and AlmaLinux; no external CDN is required.
- Removed the observed public IP from the Dashboard API and UI; it remains available through local SSH administration.
- Added monthly price and expiry / remaining days to the node card when configured.
- Reworked CPU / memory / disk / monthly-traffic into a compact 2×2 metric area with independent visual accents.
- Added three compact summary blocks for real-time rate, billing-cycle traffic totals and plan/expiry information.
- Added a dedicated dark "线路质量" panel with latency, packet loss and 30-sample history bars.
- Distinguishes unavailable probes (N/A) from timeout state.
- "未设置配额" is explicit instead of showing ambiguous dashes.
- Refined mobile spacing so a single node card remains dense but readable on iPhone-sized screens.

## Compatibility

- No database schema change.
- Existing v0.4.0-alpha Server data can be reused.
- Existing Agent policy and security boundaries are unchanged.
