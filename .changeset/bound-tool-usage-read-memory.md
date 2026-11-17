---
"server": patch
---

Tool usage insights reads now cap their ClickHouse memory and spill large aggregations to disk, and a request runs at most three of them at once. A wide window on a large project no longer exhausts ClickHouse memory and fails other queries on the server.
