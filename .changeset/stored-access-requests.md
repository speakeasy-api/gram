---
"server": minor
"dashboard": patch
---

Access requests can now be stored and decided. With the access requests rollout on, requesting access records a pending request that an organization administrator can approve as a temporary grant or deny, through the new listAccessRequests, decideAccessRequest and withdrawAccessRequest endpoints. Scopes that cannot be given temporarily can no longer be requested while the rollout is on. Administrators are still emailed.
