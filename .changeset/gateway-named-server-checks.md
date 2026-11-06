---
"server": patch
---

Gateways record an authorization check only for the servers a call names. Admitting and listing members, and filtering a server's tools, no longer record a denied `mcp:connect` check for every private server the caller has no grant for, so an agent's Failed authorization checks show only real denials. A request records each named server's check once.
