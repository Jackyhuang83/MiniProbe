# MiniProbe v0.4.4-alpha

Security and correctness release based on real-world validation after v0.4.3-alpha.

## ICMP carrier probe correctness

- Fixed a concurrency bug where raw ICMP sockets could observe replies from another carrier probe running at the same time.
- Every ICMP reply is now accepted only when its source IP exactly matches the carrier target being measured.
- Telecom / Unicom / Mobile probes now use distinct ICMP identifier spaces in addition to source-IP validation.
- Added regression tests for carrier identifier separation and reply-source isolation.
- TCP and UDP probe behavior is unchanged.

## Dashboard trusted-device login

- A successful Password Protected Dashboard login now trusts the current browser/device for 30 days instead of 24 hours.
- Trusted-device sessions are stateless HMAC-signed cookies, so a normal MiniProbe Server restart no longer forces another password login.
- The cookie remains `HttpOnly` + `SameSite=Strict`; HTTPS requests additionally receive the `Secure` attribute.
- Changing the Dashboard password automatically invalidates existing trusted-device cookies because the session signature is bound to the current password hash.
- Manual logout deletes the browser cookie immediately.
- The login screen now states the 30-day trusted-device behavior.

## HTTPS / secure public access

- Direct `http://IP:28888` remains available for first deployment and recovery, but is now explicitly labeled as unencrypted and not recommended for long-term public exposure.
- The SSH menu now presents option 8 as `安全访问（HTTPS / Cloudflare Tunnel）` and marks Cloudflare Tunnel HTTPS as the recommended production path.
- The Dashboard displays a warning banner when it is being accessed over unencrypted HTTP.
- Forwarded HTTPS headers are trusted only from a loopback proxy, preventing arbitrary Direct-mode clients from spoofing `X-Forwarded-Proto: https`.
- Existing Cloudflare Tunnel behavior is preserved: after HTTPS health verification and Agent migration, MiniProbe binds to `127.0.0.1:28888` and the public IP port is no longer exposed.

## Upgrade reliability

- Server installer default release updated to `v0.4.4-alpha`.
- systemd upgrades now explicitly restart an already-running `miniprobe-server` service after atomic binary replacement, so a one-line upgrade immediately runs the new binary.
- Agent version updated to `0.4.4-alpha`.
