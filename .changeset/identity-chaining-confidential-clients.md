---
"server": patch
---

Identity chaining preparation now reports `manual_setup_required` for public clients (`token_endpoint_auth_method` `none`), since ID-JAG redemption is limited to confidential clients. `attachUserSessionIssuer` and `detachUserSessionIssuer` now refuse to bind or unbind an organization-level client on an organization-level user session issuer from a project, as the identity commit already did, because that binding is shared by every project's servers.
