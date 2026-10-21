---
"server": patch
---

MCP route bases are now a typed value with two served surfaces, `/mcp` and the organization gateway's `/mcp/gateway`. A login state cached with any other route base, such as the retired `/x/mcp`, now fails closed on resume instead of building URLs on an unserved surface.
