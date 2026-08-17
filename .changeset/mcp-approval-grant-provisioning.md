---
"server": patch
---

Enabling the MCP approval feature now provisions the built-in `mcp_approval:read` and `mcp_approval:decide` grants onto the organization's admin system role, mirroring how Skills enablement provisions its grants. Organizations whose roles were seeded before these scopes existed received the feature flag without the grants, so every approval surface answered 403 under RBAC; re-enabling the feature repairs them.
