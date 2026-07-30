---
"server": minor
---

Toolsets with a user_session_issuer no longer accept the OAuth proxy endpoints. Previously attaching an issuer only stopped new proxy authorizations while clients holding proxy refresh tokens kept exchanging them indefinitely, outside the issuer consent, session duration, revocation and RBAC. The proxy token endpoint now answers 400 invalid_grant so clients discard those tokens and re-authorize against the issuer; authorize and register return 404, and discovery already points migrated servers at the issuer endpoints.
