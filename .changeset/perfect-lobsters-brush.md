---
"@gram-ai/elements": patch
---

feat: Add `gramEnvironment` config option to specify which environment's secrets to use for tool execution. When set, sends the `Gram-Environment` header to both MCP and completion requests.
