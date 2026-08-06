---
"server": minor
"dashboard": minor
---

The External Services page is now organization-scoped: org admins register how Gram authenticates into their own cloud account, behind a new `customer_managed_encryption_keys` entitlement enforced on both `externalCredentials` and `externalKeys`. The platform-admin UI is removed, though its endpoints remain for HTTP-only management. Two new methods support verification: `externalCredentials.verifyGcpIam` probes that Gram can actually impersonate the named service account, and `externalCredentials.getGcpSetupInfo` reports the Gram service account a customer must grant `roles/iam.serviceAccountTokenCreator` to.
