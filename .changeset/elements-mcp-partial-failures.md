---
"@gram-ai/elements": patch
---

Stop one failing MCP server from dropping tools from healthy ones. When a chat is configured with multiple MCP servers, a rejection on any single `tools/list` call (e.g. a 401 on one toolset) used to wipe out the merged tool map and trigger React Query retries against every server. Healthy servers are now merged in normally; only when every server fails does the query reject and retry.
