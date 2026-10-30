---
"server": minor
---

Inbound EMA accepts ID-JAGs with no `resource` claim, like the ones Okta issues. The token request then names the MCP server in its `resource` parameter, and the session works only for that server. An inbound-only trust link now needs an identity provider with a JWKS URL. Metadata stops advertising the ID-JAG grant when that URL is missing.

Token requests on MCP authorization servers that name no `resource` where one is required now get `invalid_target` (RFC 8707 §2) instead of `invalid_request`, matching the authorization endpoint.
