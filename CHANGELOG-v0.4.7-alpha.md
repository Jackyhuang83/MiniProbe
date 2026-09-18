# MiniProbe v0.4.7-alpha

Correctness refinement for IP nature labels plus a small plan-metadata improvement for long-term renewable nodes.

## More conservative IPv4 / IPv6 nature labels

- Keeps the compact family badges from v0.4.5/v0.4.6: `V4`, `V4 NAT`, and `V6`.
- Reworks IP nature into two independent dimensions instead of one mutually-exclusive guess:
  - network nature: `家宽`, `IDC`, or `移动` only when there is specific evidence;
  - regional nature: `原生` or `广播` only when location evidence is sufficiently consistent.
- `家宽` no longer matches generic carrier/company names such as Telecom, Unicom, NTT, KDDI or SoftBank. Those organizations also own transit and datacenter ranges, so their names alone are not residential evidence.
- Explicit `is_datacenter` and `is_mobile` intelligence takes priority. Residential classification requires fixed-access wording such as residential/broadband/fiber/cable/FTTH/DSL while datacenter/mobile/VPN/Tor/proxy flags must be clear.
- `原生` / `广播` now requires two independent GeoIP country results to agree before comparing against registration country.
- RDAP country extraction now reads only the top-level RFC 9083 `ip network` object's `country`. Nested registrant/contact entity countries are deliberately ignored.
- If the two GeoIP sources disagree, RDAP lacks a top-level country, or any lookup fails, MiniProbe omits the uncertain regional label rather than guessing.
- Compact combinations include examples such as `V4 家宽·原生`, `V4 IDC·广播`, `V6 移动·原生`; when evidence is incomplete the base label remains `V4` / `V6`.
- Public IP addresses themselves remain hidden from the Dashboard.

## Long-term renewable plan marker

- Node expiry input now accepts `L`, `long`, `longterm`, or `长期` as a long-term renewable marker.
- The marker is stored as the compatibility date `2036-01-01`, so no database schema migration is required.
- Dashboard renders that sentinel as `长期` instead of a large remaining-day count.
- Existing normal `YYYY-MM-DD` expiry dates continue to show remaining days.
- Expiry input is now calendar-validated; malformed dates are rejected before saving.
- Partial node editing semantics from v0.4.5 remain unchanged: Enter keeps the current value and `-` clears optional fields where shown.

## Line quality

- Keeps the v0.4.6 20-round rolling packet-loss/failure percentage so the number and the visible history track use the same measurement window.

## Version

- Agent version updated to `0.4.7-alpha`.
- Server installer default release updated to `v0.4.7-alpha`.
