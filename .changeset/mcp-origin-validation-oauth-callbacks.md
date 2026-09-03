---
"server": patch
---

Fixed MCP `Origin` validation rejecting the OAuth callback routes registered under `/mcp/` and `/x/mcp/`. `idp_callback` and `remote_login_callback` sit in the same path position as a server slug, so they were treated as MCP endpoints and answered with 403 when the browser followed the upstream identity provider's redirect back to Gram. The hashed consent and install page scripts were misclassified the same way. Origin validation on the MCP JSON-RPC endpoints themselves is unchanged.
