---
"server": patch
---

MCP install pages no longer ask for a GRAM API key on private servers whose identity is delegated to a `user_session_issuer` (the newer OAuth scheme). Previously `resolveSecurityMode` only recognized the legacy `oauth_proxy_server_id` / `external_oauth_server_id` fields, so an issuer-gated private server fell through to the Gram-key prompt even though OAuth handles authentication. The check now also honors the `user_session_issuer` on the toolset and on the bridging `mcp_server`, matching the public serve path.
