---
"server": patch
---

Route authorization challenge logging through Pub/Sub before persisting events
to ClickHouse. This decouples authorization request paths from ClickHouse
availability and makes message redelivery idempotent by challenge ID.
