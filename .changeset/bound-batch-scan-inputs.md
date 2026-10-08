---
"server": patch
---

Bound batch risk scan inputs to 50 KiB when they are loaded, so one oversized message no longer fails its whole analysis batch.
