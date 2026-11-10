---
"server": patch
---

Adds per-organization envelope encryption for new features: each organization gets its own key encryption key in KMS, minted by Speakeasy on first use or supplied by the customer, wrapping one data encryption key per use case. An hourly sweep probes those keys, suspends organizations whose key became unusable, rolls back a key switch whose new key stopped working, and disables Speakeasy-managed keys a week after nothing uses them. It stays off until `GRAM_ORG_ENCRYPTION_CREDENTIAL_ID` is set, and no existing data moves to it.
