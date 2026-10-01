---
"server": minor
"dashboard": minor
---

Each organization gains one token endpoint for the workload grant, at `/o/<org-slug>/token` with RFC 8414 metadata at `/.well-known/oauth-authorization-server/o/<org-slug>`. A workload exchanges its platform identity token there for a session on any of the organization's MCP servers its assigned agent may reach, naming the server by its MCP URL in `resource`; each session is still bound to that one server. The new `workloadIdentities.organizationConnectionDetails` method reports the endpoint, its issuer and whether an exchange can succeed, and the Access Hub shows that single endpoint in place of the per-server picker. Gated per organization by the `gram-org-token-endpoint` flag.
