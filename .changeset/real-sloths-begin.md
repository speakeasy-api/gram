---
"server": minor
"dashboard": patch
---

Remote session issuers can be bound to a tunneled MCP server, so Gram routes issuer metadata discovery and back-channel OAuth calls through the tunnel when the authorization server is unreachable from the public internet. Platform admins can manage the binding from the issuer settings page; tunnel bindings and tunneled dynamic client registration remain platform-admin-only.
