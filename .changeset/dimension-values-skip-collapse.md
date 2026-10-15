---
"server": patch
---

Filter dropdowns on Explore dashboards load again on projects with a lot of agent activity. Listing a field's values no longer sorts every event in the window first, which ran ClickHouse out of memory when a page loaded several filters at once.
