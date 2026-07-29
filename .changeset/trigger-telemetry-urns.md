---
"server": patch
---

Trigger delivery telemetry logs now record a proper `trigger-instance:<uuid>` URN and the active trace context instead of a generic `urn:uuid:` identifier with empty trace and span ids.
