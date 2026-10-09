# MiniProbe v0.4.17-alpha

## Fix: prevent unrelated local Server from hijacking Agent reporting

- Fix a P1 offline issue when an Agent host also runs a different MiniProbe Server on `127.0.0.1:28888`.
- Before choosing the local-only reporting endpoint, authenticate the Agent bearer token against the local Server. An unauthorized local Server is ignored; the Agent retains its configured HTTPS endpoint.
- The preflight uses deliberately incomplete JSON and validates MiniProbe's authenticated `400 bad json` response. It does not mutate node/traffic state.
- Retain the existing local-only behavior for correctly authenticated Server-host Agents, avoiding public DNS / Tunnel hairpin dependencies.
- Add regression tests for valid and invalid local tokens, unexpected 400 responses, and unhealthy local endpoints.
- Target Agent version `0.4.17-alpha` (new Agent release required). No manual Token rotation and no traffic counter reset.
