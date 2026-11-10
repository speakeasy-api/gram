---
"server": patch
"dashboard": patch
---

Admitting a workload now refuses a malformed `wimse://` or `spiffe://` subject: it needs a trust domain and no user information, port, query or fragment, so a rule that could never match is caught when it is saved. Withdrawing an admitted subject now returns `withdrawn_tier`, which says whether the organization or project admission was removed.
