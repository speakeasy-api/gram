---
"server": minor
---

Add organization-scoped `externalCredentials` management API for CRUD of external credentials (AWS/GCP IAM) used to authenticate Gram into a customer cloud account. Per-provider create/update/get/delete plus a generic supertype-only list with an optional provider filter. Gated on `org:read`/`org:admin` and audited under per-provider subjects (`aws_iam_credential`, `gcp_iam_credential`).
