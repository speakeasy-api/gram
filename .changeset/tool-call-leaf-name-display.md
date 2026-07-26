---
"dashboard": patch
---

Tool calls in chat logs and the assistant thread now display the leaf tool name (e.g. `resolve-library-id`) instead of the fully namespaced MCP name (e.g. `mcp__context7__resolve-library-id`), matching the tool-logs table. The raw name is still used for approval logic; name search formats identically so highlight navigation stays aligned.
