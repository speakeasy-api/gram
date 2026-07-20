---
"server": patch
"dashboard": patch
---

Make the cost explorer's breakdown machinery treat the "(unset)" bucket as a first-class group everywhere, fixing the hidden Account Type breakdown on drilled slices that mix classified and unclassified spend (DNO-425).

Server: telemetry.query's dimension_values now keeps the '' bucket for every groupable dimension — it is the "(unset)" row a breakdown by that dimension renders, so consumers can count it. Only dimensions where '' means "not applicable" (the Claude attribution cuts and query_source, flagged in the dimension registry) still drop it. Empty role/group arrays likewise surface as the "(unset)" bucket.

Dashboard: the breakdown axis is resolved against the slice's actual group counts by one shared resolver, at drill time (using the clicked row's dimension values) and on load — a division whose spend all sits in one department lands directly on its users with no Department selector, while a division splitting into a named department plus department-less spend keeps the Department cut (previously hidden). The entity/detail query no longer depends on the axis (removing an internal resolution cycle), grouped queries wait for the resolved axis instead of fetching twice, a `?by=` naming a pinned or un-splittable dimension falls back to the level's default, and the URL is rewritten in place whenever the rendered axis diverges from `?by=` so links always reflect the view.
