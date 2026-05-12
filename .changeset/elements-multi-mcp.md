---
"@gram-ai/elements": minor
---

`mcp` config now accepts an array of MCP servers in addition to a single URL. Tools from multiple servers are merged and namespaced as `<name>__<tool>` so identical tool names from different servers don't collide. Each entry can carry its own `gramEnvironment` to override the top-level value.
