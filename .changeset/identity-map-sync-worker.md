---
"server": patch
---

Add the identity map sync worker: a scheduled Temporal workflow rebuilds the
ClickHouse identity_map fold table from the Postgres directory every 15
minutes, mapping each unambiguous directory or linked-account email to its
owning user. Full refresh into a staging table with an atomic swap, so
deletions propagate and readers never observe a partial map. Nothing reads the
map yet; analytics folding lands separately.
