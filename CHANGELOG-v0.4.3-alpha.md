# MiniProbe v0.4.3-alpha

Dashboard visual consolidation based on real iPhone testing of v0.4.2-alpha. This release is intentionally UI-focused and does not add monitoring features.

## Dashboard layout

- Compressed the mobile header, node header, resource grid, summary cards and line-quality panel to increase information density without materially reducing readability.
- Kept the existing card-within-card visual hierarchy for the realtime/month/package boxes and line-quality panel.
- Moved the online status to the right side of the node title row and removed the duplicated status dot from the left side.
- Moved the distro logo next to the OS/version metadata, where it belongs semantically.
- Long node names remain single-line with ellipsis instead of increasing card height.
- Increased secondary-text contrast slightly for better readability on iPhone screens.
- Enabled tabular numerals so changing percentages, rates, latency and traffic values do not visually shift the layout.

## Resource metrics

- Replaced the mixed text/emoji-style resource symbols with one consistent set of inline line icons for CPU, memory, disk and traffic.
- Aligned metric labels, values, progress bars and detail lines to a consistent baseline and spacing rhythm.
- When no traffic quota is configured, the percentage now shows an em dash and the empty quota track uses a subdued segmented style instead of looking like a 0% measurement.
- Removed duplicate "未设置" wording from the traffic metric.

## Realtime / monthly / package cards

- Preserved all three nested cards while reducing their height and internal padding.
- Simplified the unconfigured package state to one clear "未设置" value.
- Price and expiry remain available inside the package card when configured.

## Domestic line-quality panel

- Preserved the v0.4.2 dual latency/loss history layout.
- Reduced vertical padding and history-block height slightly for a denser reference-style presentation.
- Kept the lightweight ICMP/TCP/UDP protocol badge in the upper-right corner.

## Upgrade behavior

- Server installer default release updated to `v0.4.3-alpha`.
- Agent version string updated to `0.4.3-alpha`; monitoring and probe behavior are otherwise unchanged from v0.4.2-alpha.
