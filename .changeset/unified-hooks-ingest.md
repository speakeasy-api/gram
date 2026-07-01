---
"server": minor
"dashboard": patch
---

Add the unified `/rpc/hooks.ingest` endpoint for third-party hook ingestion while preserving existing provider-specific hook endpoints. Hook plugins now authenticate each developer locally through the browser callback flow and store a hooks-scoped key on the device.
