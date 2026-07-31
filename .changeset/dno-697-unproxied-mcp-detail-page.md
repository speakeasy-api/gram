---
"server": minor
"dashboard": patch
---

Unproxied MCP servers now show sensible Settings and Inspect tabs instead of the ones built for proxied servers: the custom-domain/slug and tool-filtering settings are hidden, Authentication shows a "Not applicable" state, and Inspect connects live to the vendor's server to discover its tools instead of trying to reach a Gram-hosted endpoint that doesn't exist for these servers.
