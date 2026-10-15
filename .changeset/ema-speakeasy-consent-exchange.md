---
"server": patch
---

The consent page now runs the live identity chaining exchange for Speakeasy-issued sign-ins, as it already does for external identity providers. A service that accepts the user shows "Connected through your identity provider" before approval, and one that refuses them shows the refusal and blocks "Give access" instead of failing on the first tool call. The credential obtained on the consent page is tied to that sign-in, and the grant created by approving it reuses it on first use, so approval does not sign a second assertion. Revoking the sign-in, rotating the issuer key or changing the binding before approval leaves the credential unused.
