---
"dashboard": patch
---

Fix the Employee Enrollment page showing enrolled employees with 0 tokens (their usage appearing under "Unknown users" instead). When a member's telemetry splits across identity keys — an opaque user_id with no email (e.g. Gram tool calls) plus their email (Claude/Cursor usage) — the id-keyed, token-less summary was shadowing the member's token-bearing email summary. `buildEmployees` now matches a member against both their id and their email and merges the results, so their tokens, activity, and linked accounts are attributed correctly and their usage is no longer orphaned into the unattributed list.
