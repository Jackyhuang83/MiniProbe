# MiniProbe v0.4.20-alpha

## Dashboard platform detection

- Render local, deterministic **inline SVG status icons**: green check for reachability, amber minus for limited, red cross for blocked, grey question for unknown. Compact platform chips and no reliance on remote icon hosting or emoji fonts.
- Display verified short country/region codes next to platform names; map common ISO-3166 alpha-3 codes to alpha-2 for compactness, omit codes when unavailable.
- Add **myTV SUPER** to Agent platform checks (10 total). Use the regional session response `region=1` as HK geographic accessibility and `region=0` as denied; avoid false positives from mere homepage HTTP 200 and mark ambiguous responses unknown.
- Continue conservative labeling: reachability or HK geographic access is **not** proof of actual playback, DRM capability, login/paid entitlements, or complete unlock.
- Add tests for malformed/unknown myTV SUPER responses; retain the bounded concurrency, 12h cadence, and low Agent footprint.
- Server and Agent target version: **0.4.20-alpha**. Validate one Agent after Server deployment before fleet upgrade.
