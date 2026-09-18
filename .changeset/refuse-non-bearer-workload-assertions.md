---
"server": patch
---

Refuse workload assertions whose JOSE `typ` header marks them as a WIMSE Workload Identity Token (`wit+jwt`) or an RFC 9068 access token (`at+jwt`). These credentials must never be exchanged as bearer grants, so they are rejected before any key set is fetched. Assertions with no `typ` or any other value verify as before.
