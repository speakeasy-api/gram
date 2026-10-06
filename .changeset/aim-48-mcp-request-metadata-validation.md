---
"server": patch
---

Standard MCP request headers (`MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name`, `Mcp-Param-*`) are no longer read as tool environment variables, and MCP server settings and function manifests now reject variable names or header display names that would require one, such as `NAME` or `METHOD`. Conflicting protocol version declarations now return `HeaderMismatch` (-32020) with HTTP 400.
