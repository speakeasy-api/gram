---
"server": patch
---

Add `search_users` and `get_user_metrics_summary` to the Platform MCP. `search_users` finds the people observed in one project's telemetry by partial identity over up to 30 days and returns masked identities, categorical activity evidence, last-seen times, and short-lived project-scoped person references; `get_user_metrics_summary` takes one of those references and returns the person's call volume, failures, the servers their tools name, and their most-failing tools without the raw identity. Both are organization-admin reads, metered on the sensitive diagnostics budget, and audited as attribution reads.
