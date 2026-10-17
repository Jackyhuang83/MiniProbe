# MiniProbe v0.4.19-alpha

## Fix: legacy Dashboard URLs survive upgrades

- Redirect historical `/ui/<old-version>/` roots, `index.html`, `network.html?id=...` and JS/CSS/SVG asset URLs to the **current** Dashboard version using a temporary HTTP 307 response.
- Preserve node-detail query arguments and explicitly disable caching of compatibility redirects. Previous-version browser bookmarks, open tabs, and cached static-resource links no longer result in 404 at the origin.
- Keep current-version immutable asset caching and dashboard authorization behavior unchanged, including Dashboard Disabled mode.
- Add regression coverage for several older versions, internal links, redirected resources, unsafe paths, and redirect-loop avoidance.
- **Server/UI-only update:** Agent target remains **v0.4.18-alpha**. Existing Agent installation and monitored monthly traffic data are unchanged.
- Existing Cloudflare edge-cached 404 responses, if any, still obey their TTL or must be cleared separately.
