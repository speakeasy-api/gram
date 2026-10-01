---
"server": minor
---

Remove the `explore` saved-query service (`explore.listQueries`, `createQuery`, `updateQuery`, `deleteQuery`). Explore saves widgets through the `widgets` service instead. The `query:*` audit actions and the `audit_log.query_event_v1` webhook event are retired rather than removed: nothing emits them any more, but audit entries already written keep their labels and webhook subscriptions to the event stay valid.
