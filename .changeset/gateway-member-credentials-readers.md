---
"server": patch
---

Gateway sign-in now resolves upstream credentials per OAuth client instead of per authorization server, so gateway members whose upstreams sign in through the same provider can each keep their own client and grant. The gateway consent page qualifies each client's grant to the member configured with it. Non-gateway endpoints that bind two clients of one authorization server now report a configuration error instead of an internal error.
