---
"server": patch
---

Reads `meta_mcp_servers.visibility` in the runtime and exposes it on the metaMcp API. A gateway can now be disabled: endpoint resolution treats a disabled gateway as absent, returning not-found the same way a disabled MCP server already does, so a caller cannot tell a disabled gateway from one that never existed.

Create accepts an optional visibility and defaults to `private`. Update leaves it alone when omitted, so a client that does not manage visibility cannot re-enable a disabled gateway by saving an unrelated field.

Gateways always require an authenticated caller, so the resolved endpoint no longer infers that from whether an issuer happens to be attached.
