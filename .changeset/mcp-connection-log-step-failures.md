---
"server": minor
"dashboard": minor
---

MCP connection log exports set to "Failed and blocked connections" or "Failed, blocked and declined connections" now also send failed sign-in steps that may still recover, such as a saved sign-in the service no longer accepts. They arrive as warnings with `speakeasy.mcp_connection.final=false`. Normal sign-in steps are not sent. Exports set to "Failed connections" are unchanged.
