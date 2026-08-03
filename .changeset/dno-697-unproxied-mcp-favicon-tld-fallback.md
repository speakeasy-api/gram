---
"server": patch
---

The default favicon fetched for a new unproxied MCP server now falls back to the vendor's registrable domain (e.g. `figma.com`) when the exact host (e.g. `mcp.figma.com`) has no favicon registered, instead of silently giving up.
