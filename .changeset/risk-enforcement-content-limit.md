---
"server": patch
---

Realtime risk enforcement now truncates scan inputs at a 50 KiB default instead of 1 MiB, with the limit adjustable through a feature flag. This reduces timeout-driven fail-open scans for large prompt attachments.
