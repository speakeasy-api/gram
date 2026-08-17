---
"server": minor
"dashboard": patch
---

MCP approval surfaces are now gated on `org:admin`, the same authorization as the Observe pages and the policies that do the blocking. The dedicated `mcp_approval:read`/`mcp_approval:decide` scope family is retired: it required per-organization grant provisioning that existing organizations never received, leaving every approval surface answering 403 in production. Org admins now work on deploy with nothing to provision. Delegable reviewer scopes can return additively if a customer ever needs non-admin reviewers.
