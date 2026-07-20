---
"server": patch
"dashboard": patch
---

Surface the unclassified ("") bucket in telemetry.query dimension_values for account_type and provider, matching the existing billing_mode exception. The cost explorer prunes drilled-in breakdown axes with fewer than two distinct values, so a slice mixing classified and unclassified spend (e.g. team + unattributed API-key usage) hid the Account Type breakdown exactly where the split matters (DNO-425).
