---
"server": patch
"dashboard": patch
---

The consent page no longer blocks "Give access" for MCP servers reached through enterprise-managed authorization. Those services have no token until access is given, so their tools can't be listed beforehand. When every service on the page is linked or uses identity chaining, the Tool access section says that tools load after access is given, and the connection starts with every tool the user's role allows.
