---
"server": minor
"dashboard": minor
---

Add Budgets v1: org-scoped per-person budget rules with CEL actor targeting over directory-synced attributes. A periodic Temporal evaluator sums each matched actor's LLM spend from ClickHouse against the rule's per-person limit for UTC calendar windows, records warning/breach events, and publishes circuit state to Redis. Rules with action=block deny the blocked user's Claude Code traffic (UserPromptSubmit and PreToolUse, before risk-policy scans) until the window resets. In the dashboard, Budgets renders as a tab on the Costs page wired to the new `spendrules` management API (rules CRUD, live actor preview, overview cards, events tab); the tab only appears when the `gram-budgets-page` PostHog flag is enabled, so the surface can be released to select users.
