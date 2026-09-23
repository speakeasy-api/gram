---
"server": patch
---

A workload subject can now be admitted, and resolve its agent, by a prefix of its `sub` rather than the whole value — but only under an issuer that permits prefix matching, which is checked on every lookup rather than trusted from write time, so clearing the issuer's permission revokes prefix rules already written. Where a prefix rule and an exact one both cover a subject, the most specific wins, which lets a fleet share one agent while individual principals are pinned elsewhere. Exact matching is unchanged and remains the default.
