# MiniProbe v0.4.18-alpha

## Network identity and platform capability checks

- Show each Agent's effective egress IPv4/IPv6 ASN and routing-AS organization where Team Cymru DNS can identify it, without exposing raw IP addresses on the Dashboard or saving them in the Server database.
- Run lightweight HTTP capability checks for ChatGPT, Gemini, Claude, Google Search, Netflix, Disney+, YouTube Premium, TikTok and Reddit every twelve hours.
- Differentiate simple reachability from fully unlocked streaming and subscription entitlement; unverified or ambiguous outcomes remain unknown.
- Keep IP-type labels conservative (V4 / V6 only); never guess residential/datacenter/native/broadcast classification.
- Cap request time and response size and concurrent platform checks; preserve the Agent billing counters, signed policy mechanism, and Server-only administration.
- Maintain wire compatibility with older Agents. New capability data requires upgrading an Agent to v0.4.18-alpha.
