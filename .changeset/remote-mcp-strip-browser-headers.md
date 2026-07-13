---
"server": patch
---

Stop forwarding browser-only headers (`Origin`, `Referer`, `Cookie`) from the inbound request to remote MCP upstreams. When the dashboard drove a remote MCP server, its `Origin` was relayed verbatim and upstreams enforcing the MCP spec's DNS-rebinding protection (e.g. Langfuse) rejected the request with 403 "Access forbidden", surfacing as "Something went wrong loading tools" in the Tools tab. Dropping these headers makes dashboard-proxied requests match those from a headless MCP client and prevents the dashboard session cookie from leaking upstream.
