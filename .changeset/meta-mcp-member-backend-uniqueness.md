---
"server": patch
---

A meta MCP server can no longer hold two members that front the same backend. `meta_mcp.addMember` rejects an MCP server whose remote, tunneled, toolset, or unproxied backend a live member of that gateway already serves, and `mcpServers.update` rejects repointing an attached server onto a backend one of its co-members already fronts. Both return an invalid-argument error; attaching the same MCP server twice keeps returning the existing conflict error.
