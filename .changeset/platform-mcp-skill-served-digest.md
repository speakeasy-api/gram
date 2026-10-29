---
"server": patch
---

Platform MCP's skill version listings now include `served_digest`, the `sha256:<hex>` digest agents see when they load that version over a hosted MCP server, so an agent can match a load in its logs to the exact version. Invalid versions, which are never served, omit it.
