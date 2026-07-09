---
"server": minor
"dashboard": patch
---

Issuer discovery now parses RFC 8414 `service_documentation`, `op_policy_uri`, and `op_tos_uri` and persists them on `remote_session_issuers` across the project, organization, and global admin surfaces.
