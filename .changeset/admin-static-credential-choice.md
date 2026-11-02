---
"server": patch
---

Connecting to an MCP server whose only credential is a static credential an administrator added records that credential as the connection's choice, so organizations that enforce upstream auth accept the connection instead of asking to reconnect. The auth client backfill maps existing unmapped choices the same way.
