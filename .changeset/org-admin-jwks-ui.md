---
"server": patch
"dashboard": minor
---

Organization admins can now manage JSON Web Key Sets from the Encryption Keys page: a new Signing Keys (JWKS) section lists an organization's key sets, a guided sheet creates one from a GCP KMS key, and each set's detail page shows its history, its published keys with activate / retire / revoke actions and a publish flow that re-points the backing key, and a settings tab for renaming and deleting the set. The `auditlogs.list` endpoint gains an optional repeated `subject_ids` filter so a resource's feed can include the child resources whose events name the child as the subject.
