---
"server": patch
"dashboard": patch
---

Fix the triggers page failing to load whenever a wake trigger has fired or been cancelled. The triggers list response advertised a status enum of `active | paused`, but wake triggers transition through `fired` and `cancelled` too, so the dashboard's response validation rejected the payload and surfaced a generic "Response validation failed" error. The status enum now includes all four states, and the triggers page renders distinct badges for fired and cancelled triggers instead of mislabelling them as "Paused".
