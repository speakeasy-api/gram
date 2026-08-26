---
"server": minor
"dashboard": patch
---

Remote session issuers can be bound to a tunneled MCP server, so Gram routes persisted issuer metadata refreshes and back-channel OAuth calls through the tunnel when the authorization server is unreachable from the public internet. Discover by URL still uses Gram's direct egress; private issuer endpoints, including the DCR endpoint, can be entered manually in issuer settings. Platform admins can manage the binding from the issuer settings page; tunnel bindings and tunneled dynamic client registration remain platform-admin-only.
