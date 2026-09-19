---
"server": minor
---

Add the `analytics` service: `analytics.query` runs a grouped or ungrouped query against a catalog dataset by field name, never by table or SQL, and `analytics.describe` serves the catalog the builder is generated from. Ships beside `telemetry.query`; nothing migrates.
