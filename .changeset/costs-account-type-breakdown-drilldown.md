---
"server": patch
"dashboard": patch
---

Surface the unclassified ("") bucket in telemetry.query dimension_values for account_type and provider, matching the existing billing_mode exception. The cost explorer prunes drilled-in breakdown axes with fewer than two distinct values, so a slice mixing classified and unclassified spend (e.g. team + unattributed API-key usage) hid the Account Type breakdown exactly where the split matters (DNO-425).

Also guard the cost explorer's `?by=` param against dimensions already pinned in the drill path: a stale link like `division~X/department~Y?by=department_name` rendered a degenerate one-row breakdown of the entity by itself, with a drill chevron that silently did nothing. Such links now fall back to the level's default axis.
