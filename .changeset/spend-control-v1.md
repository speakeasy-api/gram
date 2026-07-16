---
"server": minor
"dashboard": minor
---

Add Budgets v1: org-scoped per-person budget rules with CEL actor targeting over directory-synced attributes. A periodic Temporal evaluator sums each matched actor's LLM spend from ClickHouse against the rule's per-person limit for UTC calendar windows, records warning/breach events, and publishes circuit state to Redis. Rules with action=block deny the blocked user's Claude Code traffic (UserPromptSubmit and PreToolUse, before risk-policy scans) until the window resets. The dashboard Budgets page is rewired from mock data to the new `spendrules` management API (rules CRUD, live actor preview, overview cards, events tab), gated behind the `gram-budgets-page` PostHog flag.
