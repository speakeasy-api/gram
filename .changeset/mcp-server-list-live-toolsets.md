---
"server": patch
---

Listing MCP servers no longer returns servers backed by a deleted toolset, and the hosted server summary now includes the toolset's description. Opening such a server by id or slug still works, so it can be found and deleted. MCP server reads now report `has_endpoint`: whether any endpoint routes to the server, including one on a custom domain that is not live yet.
