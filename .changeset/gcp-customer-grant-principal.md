---
"server": minor
"dashboard": patch
---

Customers now grant one Speakeasy principal on their GCP service accounts, and it can be a Google group that fronts Speakeasy's service account. Set `GRAM_GCP_CUSTOMER_GRANT_PRINCIPAL` to the IAM member, such as `group:<email>`; without it the running service account is reported as before. `externalCredentials.getGcpSetupInfo` returns the full member string in a new `principal_member` field, and the dashboard's gcloud and Terraform snippets use it verbatim instead of always writing `serviceAccount:`. Existing grants to the service account keep working because it is a member of the group.
