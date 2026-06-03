---
"server": patch
---

Prepare RBAC grants for issuer-gated private remote MCP servers so `tools/list` and `tools/call` no longer fail for RBAC-enforced callers. Previously the issuer-gated path skipped grant preparation, causing the proxy's `mcp:connect` interceptors to reject the request with a missing-grants error and return zero tools.
