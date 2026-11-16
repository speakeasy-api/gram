---
"server": minor
"dashboard": minor
---

MCP connection log exports set to "Warnings and errors" or "Info, warnings and errors" now also send failed sign-in steps that may still recover, such as a saved sign-in the service no longer accepts. They arrive as warnings with `speakeasy.mcp_connection.final=false`. Normal sign-in steps are not sent. Exports set to "Errors only" are unchanged.
