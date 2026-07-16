---
"server": patch
---

Data migration translating organizations still on the removed `observability_mode` product feature to `hooks_fail_open` (DNO-497): the new fail-open row preserves the outage tolerance those orgs opted into, and the retired observability_mode rows are soft-deleted.
