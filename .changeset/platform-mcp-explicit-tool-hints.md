---
"server": patch
---

Every Platform MCP tool now states `readOnlyHint`, `destructiveHint`, and `openWorldHint` explicitly for every audience, set from what the tool really does, and keeps the same hints whether or not its feature is switched on. A tool is marked open world only when it reaches a host outside Speakeasy, such as a registry, the user's own MCP server, an OAuth provider, or GitHub. Clients such as ChatGPT's app review refuse a tool that leaves a hint implicit.
