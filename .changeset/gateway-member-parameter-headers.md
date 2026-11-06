---
"server": patch
---

Gateway calls to a server's tool now send the `Mcp-Param` headers that its input schema requires, so tools that mirror arguments into headers no longer fail with a header mismatch.
