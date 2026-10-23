---
"server": minor
---

The Platform MCP `get_mcp_connection_settings` tool now reports whether an MCP server's upstream needs authentication and which auth clients it offers, and `get_mcp_access` reports whether each role may use each shared credential the server offers. Staff server-health inspection lists a server's auth clients and its upstream authentication state, without names or header values.
