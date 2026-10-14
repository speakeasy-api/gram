---
"server": patch
"dashboard": patch
---

The consent page no longer blocks "Give access" for MCP servers reached through enterprise-managed authorization. Those services have no token until access is given, so their tools can't be listed beforehand. When every service on the page uses identity chaining, none is connected, and there is no saved tool selection, the Tool access section says that tools load after access is given, and the connection starts with all tools available to the user. If every identity chaining check is refused, approval is blocked again.
