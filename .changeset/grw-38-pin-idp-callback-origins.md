---
"server": patch
---

Pin the remaining URLs that external systems store to the outbound callback origin (`GRAM_OUTBOUND_CALLBACK_URL`) instead of the server URL: the MCP login IdP callbacks (`/mcp/idp_callback`, `/x/mcp/idp_callback`, including the federated callback sent to customer IdPs), the Platform MCP IdP callback, and the assistant MCP auth CIMD `client_id` and `redirect_uri`. Federated logins and the remote login browser hop now treat the pinned origin as the callback cookie host. With the default configuration nothing changes.
