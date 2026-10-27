---
"server": minor
---

Inbound EMA now accepts ID-JAGs without a `resource` claim, such as the ones Okta issues. The token request must then name the MCP server in its `resource` parameter, and the session is bound to that server. Inbound EMA requires an organization MCP Authentication in shared mode whose trusted identity provider publishes a JWKS URL: metadata advertises the ID-JAG grant only then, and linking a trusted identity provider without a JWKS URL, or without a trusted client on an MCP Authentication not in shared mode, is refused.
