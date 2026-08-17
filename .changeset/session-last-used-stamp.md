---
"server": patch
---

Record `last_used_at` on user sessions and remote sessions when they carry traffic, so a connection that has not been used is distinguishable from one whose token merely keeps refreshing. Writes are coalesced to a five-minute window on both token paths and are best-effort, so they never fail a request that holds a valid credential.
