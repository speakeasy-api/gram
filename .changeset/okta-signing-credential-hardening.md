---
"server": patch
---

Platform GCP credentials that impersonate a service account in Speakeasy's own project now record the own-project exemption on create and update, so the identity provider signing credential the runbook creates is usable. Creating, updating and deleting platform credentials requires a fresh platform admin session. A new `GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT` setting pins which service account the signing credential may impersonate.
