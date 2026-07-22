---
"server": minor
---

Add organization-scoped `externalKeys` management API for CRUD of external keys (AWS/GCP KMS) Gram signs with, each backed by an external credential. Per-provider create/update/get/delete plus a generic supertype-only list with an optional provider filter. Gated on `org:read`/`org:admin` and audited under per-provider subjects (`aws_kms_key`, `gcp_kms_key`).
