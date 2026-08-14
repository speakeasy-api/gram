---
"server": patch
---

The admin API can now report how big the platform is in one call. `GET /admin/organizations.stats` returns the number of organizations in total, the number created in the last 7 days, the number whose enterprise trial is ending soon, the number disabled, and the number disabled in the last 7 days. The total and both 7-day figures include disabled organizations, so the total reports the real platform size rather than the organizations list's default active-only view. None of the five figures narrows to the caller's list filters, so they stay put while an operator filters the list. The ending-soon figure is counted from the same trial state definition the organizations list filters on, so the figure agrees with the rows an operator lands on after clicking it.
