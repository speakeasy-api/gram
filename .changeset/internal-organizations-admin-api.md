---
"server": patch
---

Staff can mark an organization as internal through the admin API and filter the admin organization list by it. Internal organizations belong to Speakeasy employees for testing and dogfooding. The label grants nothing; it is mirrored to PostHog as the `is_internal` property on the organization group so analytics can leave these organizations out.
