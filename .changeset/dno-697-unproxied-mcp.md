---
"server": minor
"dashboard": patch
---

Add unproxied MCP servers: Speakeasy staff can register a vendor's MCP server that Speakeasy lists and can attach to a plugin without ever proxying it, so there's no OAuth callback or upstream allowlisting to negotiate. The vendor URL is validated (scheme, host, SSRF blocklist) at creation, and the server's icon defaults to the vendor's favicon (falling back to its registrable domain when the exact host has none), replaceable later from Branding settings like any other server.

The Overview tab shows a best-effort usage chart plus a tabbed, paginated breakdown of tools/users/clients, both sourced from Shadow MCP's hook-reported activity matched by URL, since Speakeasy's own proxy telemetry never sees traffic to these servers. Settings hides fields that don't apply (custom domain, tool filtering), marks Authentication "Not applicable", and shows a fixed "Public" visibility indicator instead of a Disabled/Private toggle that gated nothing. The sidebar shows the vendor's upstream URL, labels the server type, and disables "Test in Playground" since the Playground can't reach a server Speakeasy never proxies. The Inspect tab is hidden for now, since there's no reliable way yet to list an unproxied server's tools without live vendor auth.
