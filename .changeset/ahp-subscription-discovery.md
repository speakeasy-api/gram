---
"server": patch
---

Advertise authenticated AHP subscription defaults through hooks/configuration. User messages and tool requests use interceptors with a 30-second timeout and the organization's current fail-open or fail-closed setting. Session, turn, model response, and tool result events use observers; turn-start observation remains an acknowledged no-op. Both modes request body content.
