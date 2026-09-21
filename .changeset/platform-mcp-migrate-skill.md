---
"server": patch
---

Add the `migrate-mcp-between-projects` skill to the Platform MCP plugin. It walks an administrator through redeploying one Platform-managed MCP server from an explicit source project to an explicit target project in the same organization: read the source shape, acknowledge what can and cannot be copied, re-register the same catalogue entry or remote URL in the target, finish secure setup and provider attachment there, verify fresh readiness, distribute to one exact plugin, and only then optionally disable the source with a rollback path.
