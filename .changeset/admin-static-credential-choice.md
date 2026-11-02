---
"server": patch
---

Connecting to an MCP server whose only credential is a static credential an administrator added now records that credential as the connection's choice. Before, the choice named no credential, so organizations that enforce upstream auth rejected every request and asked to reconnect, which recorded the same choice again. The auth client backfill maps choices already recorded this way.
