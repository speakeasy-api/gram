---
"tunnel": minor
"server": minor
"dashboard": minor
---

A stdio MCP server behind the tunnel agent can act upstream as each Speakeasy user. With `TUNNEL_STDIO_CREDENTIALS=user` and the `TUNNEL_IDENTITY_*` verifier settings, the agent verifies every request's signed caller assertion, binds each session to one user, MCP server and upstream grant, and writes that user's access token to a per-session file on a memory-backed filesystem, named by `SPEAKEASY_ACCESS_TOKEN_FILE`. Sessions end when the credential is unlinked, reauthorized, or expires.

Caller assertions for private tunnels now carry `mcp_server_id` and, when Speakeasy forwards an upstream credential it resolved, an `upstream_credential` claim with the grant identity and the SHA-256 of the forwarded token. The claim version stays `1`.

The tunneled MCP stdio setup snippets can enable per-user credentials, filling in the verifier settings and a memory-backed volume.
