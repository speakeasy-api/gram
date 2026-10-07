---
"server": minor
"dashboard": minor
---

The Access Hub's guided setup now shows the token endpoint, issuer and MCP host an external platform is pointed at. A dropdown lists every user session issuer in shared mode, at the organization level and in each project, by slug, and the values shown are that issuer's `/oauth/usi/{id}` authorization server. They come from the new `workloadIdentities.listTokenEndpoints` method, which requires `workload:read` and derives them exactly as the shared authorization server's own metadata does.
