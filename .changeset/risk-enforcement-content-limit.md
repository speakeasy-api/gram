---
"server": patch
---

Realtime risk enforcement now applies a configurable per-organization content limit before dispatch, with a safer 50 KiB default. This reduces timeout-driven fail-open scans for large prompt attachments.
