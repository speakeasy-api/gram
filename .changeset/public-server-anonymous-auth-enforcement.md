---
"server": patch
---

Public MCP servers keep serving people who connect without signing in when upstream auth enforcement is on. Each of them is served the auth client the server resolves for them, including their own connection to the upstream service, as before enforcement. A personal connection that no signed-in user owns is now refused with an authorization error instead of failing the request.
