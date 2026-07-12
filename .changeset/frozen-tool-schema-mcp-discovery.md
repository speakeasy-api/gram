---
"server": minor
---

Assistants no longer send every MCP tool schema to the model on every call. MCP tools are discovered on demand through a search tool, MCP servers connect on first use instead of during assistant startup, and dropped MCP connections reseat automatically instead of requiring a reconnect tool call. This keeps provider prompt caching effective for assistants with large toolsets and removes MCP handshakes from assistant cold-start latency.
