---
"server": minor
---

Each MCP server's access list now reports who can actually connect once every rule is applied: which members a rule lets in, which connect only because a rule naming them outranks a block they inherit, and which a block stops. Changing a server's access list is now recorded in the audit log. The Platform MCP `get_mcp_access` tool reports the server's connection audience, and the new `set_mcp_connection_audience` tool lets an administrator allow or block everyone, one role or one member on one server, with confirmation and a version check.
