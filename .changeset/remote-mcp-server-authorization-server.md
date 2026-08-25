---
"server": patch
---

Schema-only expansion for direct upstream authorization on MCP servers. Nothing reads or writes the new columns yet, so behavior is unchanged.

`mcp_servers.remote_session_issuer_id` lets a server delegate authorization to an upstream — the client authorizes there and its bearer is forwarded verbatim, with Gram out of the loop. It is mutually exclusive with `user_session_issuer_id`, and is the `mcp_servers`-side replacement for `toolsets.external_oauth_server_id`. `remote_session_issuers.metadata` stores the discovery document verbatim, because the typed columns model only what Gram acts on and re-serving from them would drop the OIDC fields they omit.
