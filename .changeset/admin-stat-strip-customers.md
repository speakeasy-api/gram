---
"server": patch
---

The first cell of the admin organizations stat strip now counts customers — organizations on a payg or enterprise account type — instead of every organization on the platform, and pressing it filters the list to those two types. The `organizations.stats` admin endpoint gains `customers` and `customers_created_last_7_days`; `total` and `created_last_7_days` are unchanged.
