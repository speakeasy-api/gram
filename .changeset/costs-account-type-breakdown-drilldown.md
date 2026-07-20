---
"server": patch
"dashboard": patch
---

Surface the unclassified ("") bucket in telemetry.query dimension_values for account_type and provider, matching the existing billing_mode exception. The cost explorer prunes drilled-in breakdown axes with fewer than two distinct values, so a slice mixing classified and unclassified spend (e.g. team + unattributed API-key usage) hid the Account Type breakdown exactly where the split matters (DNO-425).

Also guard the cost explorer's `?by=` param against dimensions already pinned in the drill path: a stale link like `division~X/department~Y?by=department_name` rendered a degenerate one-row breakdown of the entity by itself, with a drill chevron that silently did nothing. Such links now fall back to the level's default axis.

The breakdown axis is now slice-aware end to end: drilling into an entity skips axes the slice cannot split (a division whose spend sits in a single department lands directly on its users, and the Department selector is not offered at all), both at drill time — using the clicked row's dimension values — and on load, where the resolved axis falls back once the slice's distinct values are known.
