---
"server": patch
---

Stop double counting Codex/ChatGPT compliance COSTS events. The feed repeats
the same `event_id` across log files, and `telemetry_logs` has no uniqueness
constraint, so each repeat was imported as its own row and inflated every
token and cost aggregate downstream. The importer now checks the
`codex.compliance.event_hash` fingerprint it already stamps against rows
already ingested for the project and drops the repeats, which also makes
re-polling a window idempotent instead of doubling it.
