---
"server": minor
"dashboard": patch
---

Remove the `value_hash` field from environment entries. It was documented as a way to identify matching values across environments, but every code path computed it from the already-redacted display value (`val[:3] + "*****"`), so it collided for any two values sharing a 3-character prefix and never reliably identified matching values. The only dashboard consumer grouped by it, and because colliding values also render identical redacted strings, the grouping was never observable. Replaced the dashboard's value-hash grouping with direct per-environment value tracking and dropped the field from the API surface.
