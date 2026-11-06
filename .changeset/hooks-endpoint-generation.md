---
"server": patch
---

Hook telemetry rows now record which endpoint generation sent them, and take the hooks binary version from the event envelope when the request headers don't carry it, including on offline-spool replays.
