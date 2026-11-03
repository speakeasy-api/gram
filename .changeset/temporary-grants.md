---
"server": patch
---

Administrators can give a member a scope for a limited time, such as an hour of `chat:read` for an investigation, without changing their roles. `access.createTemporaryGrant` sets a deadline between five minutes and 30 days away. Once it passes the grant stops authorizing new requests, with no sign-out or role removal. `access.listTemporaryGrants` shows each grant's boundary, remaining time and the other grants that would keep the same access. `access.renewTemporaryGrant` and `access.revokeTemporaryGrant` move or end a grant without touching permanent grants. Each change is recorded in the audit log, including whether administrators gave the access to themselves.
