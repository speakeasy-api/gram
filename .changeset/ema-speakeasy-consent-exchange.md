---
"server": patch
---

The consent page now runs the live identity chaining exchange for Speakeasy-issued sign-ins, as it already does for external identity providers. A service that accepts the user shows "Connected through your identity provider" before approval, and one that refuses them shows the refusal and blocks "Give access" instead of failing on the first tool call. A refusal right after a signing key is published reads as neutral, since the service may not have fetched the key yet. The credential obtained on the consent page is tied to that sign-in, and a grant backed by the same sign-in reuses it on first use instead of signing a second assertion. Approving before the check finishes keeps the earlier behavior: the first tool call runs the exchange. Revoking the sign-in, rotating the issuer key or changing the binding before approval leaves the credential unused, and a consent check never replaces another grant's stored credential.
