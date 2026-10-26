---
"server": patch
---

A toolset now counts as hosted only through its hosted MCP server. A toolset without one is no longer served at its legacy `/mcp/<slug>` address, no longer counts as an enabled, public or private server, and no longer appears in admin server lists or as a user session issuer owner. The external MCP migration tool now deletes such toolsets instead of blocking on them. Nothing writes the toolset MCP hosting columns any more, and the retired hosted MCP wrapper backfill tool is removed.
