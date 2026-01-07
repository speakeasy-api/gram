---
"server": patch
---

feat: Support external MCP servers that only have an SSE remote available.

Previously, Gram could only support external MCP servers that used the 
Streamable HTTP transport. Now, servers that still use the deprecated SSE 
type will be transparently adapted to Streamable HTTP. MCP clients will
still use Streamable HTTP to interact with the external MCP server via Gram:

```
CLIENT <-(Streamable HTTP)-> GRAM <-(SSE)-> EXTERNAL MCP SERVER
```
