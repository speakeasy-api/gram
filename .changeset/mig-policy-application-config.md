---
"server": patch
---

Add a nullable `application_config jsonb` column to `risk_policies`: a
self-describing `{ include, exempt }` predicate set that narrows which messages
a policy evaluates (generalizing `message_types`, now deprecated) and exempts
matched messages inline. Schema-only.
