---
"server": patch
---

Record background work in the audit log as the `system` acting surface instead of `unknown`, so an unknown surface once again means a request we could not attribute rather than a scheduled job.
