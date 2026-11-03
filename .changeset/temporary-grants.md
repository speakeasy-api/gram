---
"server": patch
---

Administrators can give a member, a role, an agent or everyone a scope for a limited time, such as an hour of `chat:read` for an investigation, without changing role assignments. `access.createTemporaryGrant` sets a deadline between five minutes and 30 days away. Once it passes the grant stops authorizing new requests, with no sign-out or role removal. `access.listTemporaryGrants` takes a principal URN and shows each of its grants' boundary, remaining time and the other grants that would keep the same access. `access.renewTemporaryGrant` and `access.revokeTemporaryGrant` move or end a grant without touching permanent grants. Each change is recorded in the audit log, including whether administrators gave the access to themselves.
