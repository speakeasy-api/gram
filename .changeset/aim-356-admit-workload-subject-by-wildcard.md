---
"server": patch
---

A workload subject can now be admitted, and resolve its agent, by a trailing wildcard such as `wimse://identity.anthropic.com/org/<ORG_ID>/agent/*` rather than the whole value — but only under an issuer that permits wildcard matching, which is checked on every lookup rather than trusted from write time, so clearing the issuer's permission revokes wildcard rules already written. The `*` is mandatory and must be last, so a rule states its own breadth instead of a bare stem quietly matching more than it appears to. Where a wildcard rule and an exact one both cover a subject, the most specific wins, which lets a fleet share one agent while individual principals are pinned elsewhere. Exact matching is unchanged and remains the default.
