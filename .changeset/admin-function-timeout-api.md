---
"server": minor
---

Staff can set how long one Speakeasy Function invocation may run for an organization with the new admin `setFunctionTimeout` method, between 30 and 600 seconds, or clear it to restore the 60-second platform default. The organization record and the staff Admin MCP organization summary return the limit in effect and whether it is the default.
