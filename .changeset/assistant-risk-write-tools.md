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
invoking user. Exact and regex match values are fingerprinted before they reach
the model, so `platform_create_risk_exclusion` reuses an equivalent existing
exclusion rather than duplicating one the model had no way to recognise.

Also keeps the assistant's context from ballooning while it triages: the
agent-facing findings listing now defaults to 25 results and caps at 50
(a 200-row page was tens of thousands of tokens that stayed in context for the
rest of the turn), and the new `platform_get_risk_rule_breakdown` answers
"which rules fire most" in one small call instead of many large pages.
