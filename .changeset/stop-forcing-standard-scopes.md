---
"server": patch
"dashboard": patch
---

Remote MCP sign-ins no longer add `openid`, `email`, `profile` and `offline_access` to scopes an operator set. A client's scopes, a pin on an MCP server and a provider's scope override are requested exactly as set. A scope set an MCP server challenges with gains only the standard scopes that server advertises, and advertised scope lists are requested as published. Providers that advertise those scopes but refuse them for a given app no longer fail sign-in with `invalid_scope`.
