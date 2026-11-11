---
"server": minor
"admin": minor
---

Staff can manage platform GCP credentials from a new Platform credentials section in admin: list them, create one, view its configuration, run a live identity check, edit it and delete it. A platform credential's identity can no longer be changed, and it can no longer be deleted, while Speakeasy-managed organization encryption keys still reach KMS through it. Renames are still allowed. Provisioning a managed organization encryption key now re-checks the platform credential inside its write, so a credential deleted or repointed mid-provision is refused rather than recorded.
