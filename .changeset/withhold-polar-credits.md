---
"server": patch
"dashboard": patch
---

Withhold Polar `credits` and `included_credits` from non-platform-admin callers of `usage.getPeriodUsage`. The fields are now optional and omitted unless `authCtx.IsAdmin` is set, matching the existing admin-only billing meter in the dashboard.
