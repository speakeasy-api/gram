---
"server": minor
"dashboard": minor
---

The MCP connections list now shows how a registered agent authenticates. `UserSessionClient` gains `credential_kind` (`public`, `secret`, `key`, or `misconfigured`) alongside the raw declared `token_endpoint_auth_method`, and `UserSession` carries the same pair for the registration a session was issued through. The kind is derived on the server by the rule the token endpoint already enforces, so a registration that predates the recorded method still resolves rather than reading as unknown, and one whose columns contradict each other is reported as `misconfigured` instead of as the method it declared.

In the dashboard, agent rows badge only the two kinds worth interrupting a scan for — key-authenticated and cannot-authenticate — while the registration detail sheet states the kind for every client and writes out the declared protocol value. That sheet is reachable again, from a "View registration" item in an agent row's menu; it had no entry point since the connections list replaced the old clients table.
