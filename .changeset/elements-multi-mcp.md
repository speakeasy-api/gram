---
"@gram-ai/elements": minor
---

Add an `mcps` config option that connects a single chat to multiple MCP servers. Tools across servers are merged and namespaced as `<name>__<tool>` so identical names don't collide; each entry can pin its own `environment` slug. When set, `mcps` takes precedence over the existing single-server `mcp` option, which continues to work unchanged.
