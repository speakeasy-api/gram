---
"server": minor
---

The ID-JAG exchange is now accepted on the authentication host, and an ID-JAG must be addressed (`aud`) to the issuer the authorization server metadata announces. For an issuer that opts in with `use_authentication_host`, that is the authentication host issuer rather than the MCP server URL, so update the identity provider's audience when turning the setting on.
