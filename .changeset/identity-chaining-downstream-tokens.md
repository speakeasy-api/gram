---
"server": minor
---

Remote and tunneled MCP servers with a ready identity chaining binding now obtain downstream access tokens from the signed-in user's identity provider (enterprise-managed authorization, Okta Cross App Access). Gram exchanges the user's retained ID token for an identity assertion grant, redeems it at the server's authorization server, and reuses the resulting token until shortly before it expires, so users are not asked to connect those servers one by one. An interactive connection still takes precedence, and servers without a binding keep their existing sign-in behavior.
