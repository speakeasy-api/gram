---
"server": minor
---

Toolset-backed MCP servers resolved through `mcp_endpoints` are now served under their `mcp_servers` row's configuration: visibility, issuer gating, the RBAC resource id for `mcp:connect` (per-server, per-tool, and the consent tool picker), and the variation-group override all come from the wrapper, and the toolset's own `mcp_is_public` / `user_session_issuer_id` columns are no longer consulted on that path. Bearer validation on toolset-backed wrappers accepts the legacy toolset-URN audience as a counted fallback (`mcp.legacy_audience_accepted`). A resolvable `mcp_endpoints` address whose backend is disabled or dangling is now a terminal not-found on every surface instead of falling back to the legacy `toolsets.mcp_slug` lookup, and every remaining legacy fallback resolution increments `mcp.toolset_slug_fallback` by entry point. No production traffic changes on deploy: no toolset-backed `mcp_servers` rows exist yet.
