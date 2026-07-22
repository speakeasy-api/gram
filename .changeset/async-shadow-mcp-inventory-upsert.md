---
"server": patch
---

Move the shadow-MCP inventory upsert off the synchronous hook request path. The capture previously ran inside SessionStart/ConfigChange handling and issued one `custom_domains` query plus one sequential ClickHouse point-SELECT per inventory entry, holding hook responses for seconds on large inventories. The whole unit now runs detached from the request, the existing-row lookup is batched into one query per project, and the org's trusted hosts are resolved once per capture. Enforcement is unaffected — the shadow-MCP guard reads the Redis snapshot, which is still written synchronously.
