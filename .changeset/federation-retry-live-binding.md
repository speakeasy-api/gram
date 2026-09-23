---
"server": patch
---

Reject federated delegation retries when the trusted issuer URL has changed, even if issuer and client IDs are unchanged. Revalidate private endpoint authority before consent actions consume retry state or access credentials, preserving consent state when authority has been revoked or repointed.
