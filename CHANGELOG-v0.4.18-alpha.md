# MiniProbe v0.4.18-alpha (development)

## Node network identity
- Show the node's directly observed egress ASN and Team Cymru's published ASN operator name.
- Cache ASN enrichment on Server and remove raw egress IP before storing Agent reports.
- Show Cloudflare-observed egress country as an estimate, without claiming IP purity or residential status.

## Bounded platform checks
- Nine checks: ChatGPT, Gemini, Claude, Netflix, Disney+, YouTube, TikTok, Google Search, Reddit.
- Distinguish strong evidence of availability from basic website reachability, limited access, explicit denial and inconclusive results.
- Scan every 12h, max three concurrent calls, each at most 7s and 128 KiB read.
- Mark stale/offline results as historical, never as live unlock.
- Preserve monthly billing traffic, node configuration, and older Agent compatibility.

## Safety
- No new Go dependencies, external scripts, remote shell or public control API.
- Avoid saving public IPs, credentials or individual platform HTTP responses.
- Do not release before go test, go vet and field regression.
