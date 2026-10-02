---
"server": patch
---

Opening an MCP server's install page from a link on another site no longer returns 403. Browser GETs to `/mcp/{slug}` that accept HTML keep safe-method semantics in the Origin check, so the install page renders; cross-site SSE GETs are still rejected.
