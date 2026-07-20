---
"server": minor
---

Add tool metadata management methods to the mcpServers service.

Two batch writes with deliberately different contracts:

- `setToolMetadataBatch` is authoritative — it upserts every tool in the payload and retires (soft-deletes) every stored tool the payload omits.
- `addToolMetadataBatch` is strictly additive — it inserts the tools in the payload, leaves stored tools absent from the payload untouched, and retires nothing. A tool that already has a live stored entry fails the whole batch with a 409 rather than being upserted or skipped, so a caller working from a stale view of stored state is told so instead of having the discrepancy silently absorbed. A tool whose only prior entry is retired is recorded fresh.

Also adds `listToolMetadata`, `setToolMetadata`, and `deleteToolMetadata`. Mutations require mcp:write, are scoped to the target MCP server, and record one collection-level audit entry per write; reads require mcp:read.
