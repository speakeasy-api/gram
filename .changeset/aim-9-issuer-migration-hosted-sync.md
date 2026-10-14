---
"server": patch
---

Migrating an organization's user session issuer now moves each hosted MCP server through its toolset, like a toolset save, and records the server change in the audit log. A hosted server that has drifted from its toolset keeps the previous issuer-only update, so the migration never changes a server's URL, name or plugin entries.
