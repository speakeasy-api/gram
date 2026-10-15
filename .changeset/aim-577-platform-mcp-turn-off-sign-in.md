---
"server": minor
---

The Platform MCP server can now turn an MCP server's sign-in off (`turn_off_mcp_sign_in`), the counterpart to connecting one. It previews what the change would do and makes none until it is called again with the user's confirmation, reports whether other MCP servers keep the provider's OAuth client, and reads the result back rather than reporting the plan.
