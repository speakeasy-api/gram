---
"dashboard": minor
"server": patch
---

Platform admins get a Support Coverage page under Platform Admin that compares integration-method capability coverage across Claude Chat, Claude Code, Cowork, Cursor, Codex, and other recognized agents. It shows 30-day aggregate telemetry and current device-agent health for the organization, and reports evidence that is loading, unavailable, or unknown as such rather than as zero coverage. The `telemetry.query` endpoint gains an optional `include_dimension_values` flag, defaulting to true, so callers that only need aggregates can omit per-row dimension values.
