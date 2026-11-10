---
"server": minor
---

Adds the orgEncryption management API for organizations with customer-managed encryption keys. Admins can register a GCP KMS symmetric key, verify it, switch the organization to it, complete or roll back the switch, rotate a use case's data key and resume after a suspension. A key that still protects data cannot be deleted, and Speakeasy-managed keys cannot be deleted at all. The externalKeys API now only ever returns signing keys, and a JSON Web Key Set refuses an encryption key.
