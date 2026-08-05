---
"server": patch
"hooks": patch
---

Stop reading an unreadable MCP inventory as proof a session has no MCP servers.

The Codex meta-tool shadow-MCP guard denies a call it cannot clear against the
session's inventory, so an empty inventory decides whether legitimate traffic is
blocked. But "the agent has no MCP servers configured" and "we could not read
the list" both arrive as zero entries, and only the sender can tell them apart —
collection is best-effort and comes back empty when the agent binary cannot be
located or the probe fails. Hook events now carry `mcp_inventory_collected`, and
the guard enforces only on a list that was actually read. Senders predating the
field omit it and keep their current behavior until they upgrade, so enforcement
arrives with the hooks release rather than depending on a server deploy and a
hooks release landing in the right order.
