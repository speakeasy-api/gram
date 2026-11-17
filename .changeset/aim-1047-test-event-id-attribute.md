---
"server": patch
"dashboard": patch
---

The data export test event ID attribute is now `speakeasy.test_event_id`, so destinations that nest dotted names, such as Datadog, keep `speakeasy.test_event=true` and filtering on it works.
