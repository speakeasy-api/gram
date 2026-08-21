---
"server": minor
---

Add the `jsonWebKeySets` management API: organization-scoped CRUD of JSON Web Key Sets backed by customer KMS keys, plus the publish/activate/retire/revoke lifecycle of their published keys. Creating a set mints and publishes its first key straight to active from the backing GCP KMS key's public half; publishing into a set with an active key enters as pending for verifier cache warm-up. Revoked kids can never be republished into a set, revocation history is readable via `listKeys` with `include_revoked`, and the whole surface is gated on the `customer_managed_encryption_keys` entitlement.
