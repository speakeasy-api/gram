---
"server": minor
---

`userSessions.listUserSessions` now returns each session's `last_used_at` and its `upstreams` — the remote sessions Gram holds for the same subject and issuer. Both legs of a brokered connection are available from one call, so a client can show what an agent connects through and what Gram reaches on its behalf without a second request per row.
