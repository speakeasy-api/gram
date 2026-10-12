---
"dashboard": minor
---

Behind the `okta-connections` flag, remote MCP servers on an organization user session issuer that trusts an identity provider sign-in client get an **Identity chaining** control. It reads the server's identity chaining state, enables it for a linked client with `prepareEMA`, and disables it with `unlinkEMA` after a confirmation explaining that everyone falls back to interactive sign-in. It shows the state and the server's remediation.

A **Requested scopes** picker is shown before enabling and while enabled (**Update scopes** re-prepares at the current generation). It defaults to the linked client's scopes minus sign-in scopes (openid, profile, email, offline_access, phone, address), shows the binding's stored scopes once enabled, and lists the server's advertised scopes, disabling any that are not on the client's scope with a link to the client's settings. An empty selection requests no scope, and the identity provider and the server's authorization server apply their defaults or may reject the request. Stored scopes are compared after removing sign-in scopes, so older bindings that stored `openid` do not keep showing **Update scopes**.

Until identity chaining is enabled, the control notes that each vendor must also enable enterprise-managed authorization in its own admin console and trust the identity provider's issuer, naming that issuer when it is known and using Okta and Linear as the example. The Cross App Access tab shows the same note with the organization's Okta issuer.

Enabling is blocked for a public client, since identity chaining needs a confidential client. For a client that publishes a Client ID Metadata Document, enabling publishes the grants in that document; for other clients with no recorded grants it declares `authorization_code` plus JWT-bearer. When the server's protected resource metadata was probed, enabling sends its resource and authorization servers so a mismatch is refused.
