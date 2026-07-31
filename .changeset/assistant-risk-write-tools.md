---
"server": patch
---

Let the project's managed assistant act on risk findings, not just read them.
Adds `platform_list_risk_exclusions` and `platform_create_risk_exclusion` for
suppressing a whole class of findings, plus
`platform_mark_risk_false_positive` and
`platform_unmark_risk_false_positive` for dismissing (and restoring)
specific findings. The writes go through the same risk service methods the
dashboard uses, so they stay gated on org admin and audited against the
invoking user.
