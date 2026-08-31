---
"server": patch
---

Hosted (toolset-backed) MCP servers resolved through `mcp_endpoints` now derive their identity from the resolved endpoint and `mcp_servers` wrapper instead of the toolset columns. The well-known OAuth documents key the OAuth slug and resource URL on the endpoint the request arrived at; session mint accepts any issuer-gated `mcp_server_id` and builds the issuer URL from the server's primary endpoint, with `toolset_id` mints resolving to the wrapper when one exists; the install page takes publicness and security mode from the wrapper (the toolset's external OAuth reference remains the only toolset input) and the install URL from the endpoint; instance MCP URLs come from the wrapper's primary endpoint. Legacy toolset paths are unchanged for servers without a wrapper, and no toolset-backed wrappers exist in production yet, so no traffic changes on deploy.
