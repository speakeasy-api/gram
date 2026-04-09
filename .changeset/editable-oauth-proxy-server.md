---
"server": minor
"@gram/client": patch
"dashboard": patch
---

Add editable OAuth proxy server configuration.

Admins can now edit an existing OAuth proxy server's audience, authorization endpoint, token endpoint, scopes, token endpoint auth methods, and environment slug without having to unlink and recreate the configuration. The new `POST /rpc/toolsets.updateOAuthProxyServer` endpoint accepts partial updates with PATCH semantics (omit fields to leave them unchanged; pass an empty array to clear array fields). The dashboard's OAuth proxy details modal now exposes an Edit button that opens the existing OAuth modal in edit mode with the current values pre-filled.

Slug and provider type remain immutable after creation. Gram-managed OAuth proxy servers stay view-only.
