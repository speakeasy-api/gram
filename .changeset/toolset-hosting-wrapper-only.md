---
"server": patch
---

A toolset now counts as hosted only through its hosted MCP server. A toolset without one is no longer served at its legacy `/mcp/<slug>` address. It also no longer counts as an enabled, public or private server, and no longer appears as a toolset-only server or as a user session issuer owner. The toolset MCP hosting fields in toolset responses come only from the hosted MCP server and its endpoints. A hosted server with no usable endpoint reports no `mcp_slug`. Nothing writes the toolset MCP hosting columns any more, and the retired hosted MCP wrapper backfill tool is removed.
