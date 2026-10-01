---
"server": minor
---

Remove the `explore` saved-query service (`explore.listQueries`, `createQuery`, `updateQuery`, `deleteQuery`), its `query:*` audit actions and its outbox event. Explore saves widgets through the `widgets` service instead; audit entries already written for saved queries keep their labels.
