---
"server": patch
---

Pin the remaining URLs that external systems store to the outbound callback origin (`GRAM_OUTBOUND_CALLBACK_URL`) instead of the server URL: the MCP login IdP callbacks (`/mcp/idp_callback`, `/x/mcp/idp_callback`, including the federated callback sent to customer IdPs), the Platform MCP IdP callback, and the assistant MCP auth CIMD `client_id` and `redirect_uri`. A federated login's IdP callback uses its trusted client's recorded callback origin when it has one, so it shares a host with that client's `remote_login_callback`; the remote login browser hop follows the callback origin recorded on the challenge. With the default configuration nothing changes.
