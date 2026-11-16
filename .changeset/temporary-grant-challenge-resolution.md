---
"server": minor
---

`access.createTemporaryGrant` takes optional `challenge_ids`: each must be an unresolved denial of the same scope, raised by the grant's recipient, on a resource the grant covers, and they are resolved together with the grant in one transaction. Authorization challenges and challenge buckets carry a new `temporary_grant` resolution with the grant's id, status (`active`, `expired` or `revoked`) and end time. `access.listScopes` reports `temporary_grantable` for each permission.
