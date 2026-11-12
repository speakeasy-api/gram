---
"server": patch
---

The `mcp.connection.steps` metric now counts only connection rows stored in ClickHouse, so organizations with logs off no longer appear in it. Each row that reached a provider is tagged with that provider when it is a known vendor, otherwise `other`.
