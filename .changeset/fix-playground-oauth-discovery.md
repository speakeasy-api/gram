---
"dashboard": patch
"server": patch
---

Fix playground OAuth discovery to use toolset-level configuration instead of removed tool-definition fields.

The frontend now detects OAuth requirements from `toolset.oauthProxyServer` and `toolset.externalOauthServer` instead of inspecting individual external MCP tool definitions (whose `requiresOauth` field was removed in a prior PR). The backend `getExternalOAuthConfig()` gains two new resolution paths — OAuth proxy providers with pre-configured client credentials (skipping DCR) and external OAuth server metadata — before falling back to the legacy tool-definition lookup for backward compatibility.
