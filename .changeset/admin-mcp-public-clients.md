---
"server": patch
---

Allow public PKCE clients (`token_endpoint_auth_method: none`) to register with the staff Admin MCP, so desktop MCP clients can connect. Clients registered with a secret must still authenticate with HTTP Basic, and public clients must present no credentials.
