---
"hooks": minor
"server": patch
---

Report MCP server inventory through a dedicated hook event. The hooks client
waits for the inventory response before dispatching the first MCP tool call,
and the server caches explicit empty inventories so they do not appear missing.
