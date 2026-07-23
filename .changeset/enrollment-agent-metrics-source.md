---
"server": minor
"dashboard": minor
---

Serve the Employee Enrollment list from the pre-aggregated `attribute_metrics_summaries` view (DNO-618). `telemetry.searchUsers` gains a `source` level: `logs` (default, unchanged) scans raw `telemetry_logs`, while `agent_metrics` reads the pre-aggregated view — canonical observed agent usage (Claude Code, Codex, Cursor, Claude Chat), keyed by email — which is far cheaper (the enrollment query drops from ~seconds to tens of milliseconds on large projects). Identities that never carry an email in the window (which have no token usage) are surfaced separately from raw logs with activity but no token counts, so unknown users stay visible.

Note the enrollment token numbers change: they now reflect the same canonical agent-usage measure the costs/billing pages use, rather than the previous raw `gen_ai.usage.*` sum that mixed in Gram-hosted completions and duplicate usage-metric rows while missing Claude Code OTEL usage. Only the enrollment list opts in via `source=agent_metrics`; all other `searchUsers` consumers are unchanged.
