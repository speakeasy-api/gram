---
"server": minor
---

Propagate retroactive risk-exclusion changes into ClickHouse: creating, updating, disabling, or deleting an exclusion now rewrites the affected findings' effective state in the ClickHouse store as well as Postgres.
