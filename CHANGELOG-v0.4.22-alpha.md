# MiniProbe v0.4.22-alpha

## Better country labels with honest attribution

- Sample one small, direct HTTPS Cloudflare edge trace per 12h Agent inspection to learn the VPS's **default egress** ISO-3166 country/region where available.
- Preserve platform/site-specific region codes where present (region_source=site); where absent, show **≈JP** (example), explicitly labeling this as a **default-egress reference**, not the platform's actual unlocked market (region_source=egress).
- Keep unknown/null country when both probes fail. No guessing from node names, ASN org, hosting company or subscriptions.
- Provide distinct subdued visual style and explanatory title/legend for the ≈ suffix, with no external icon dependencies.
- Cover provenance and absent/invalid loc codes in Go tests. Existing Server/Agent privacy and traffic accounting unchanged.
- Both Server and Agent target v0.4.22-alpha. Stack on v0.4.21-alpha Server update menu; release v0.4.21 first and merge v0.4.22 second.
