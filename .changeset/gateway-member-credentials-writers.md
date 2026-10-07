---
"server": patch
---

Behind the `gateway-member-credentials` feature flag, a gateway whose sign-in issuer it owns exclusively now binds each member's own OAuth client, so members that sign in through the same provider keep separate grants. Saving the gateway reconciles missing member clients, removing a member detaches only that member's client, and an issuer holding per-member clients cannot be shared with another server, toolset or gateway. Moving or deleting a gateway clears those per-member clients from the issuer it leaves. Issuer migration now refuses merges that would bind two clients of one provider.
