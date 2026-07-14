---
"hooks": patch
"server": patch
---

Restore Claude MCP inventory capture in the Go hooks relay. Session start and configuration-change hooks now send a locally redacted inventory snapshot through canonical ingest so external MCP URLs appear in Shadow MCP inventory before a tool is called.
