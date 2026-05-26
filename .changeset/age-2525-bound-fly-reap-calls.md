---
"server": patch
---

Make the assistant-runtime reaper resilient to Fly Machines API calls that hang on missing machines. Each Destroy/List call is now bounded by its own timeout, and the Temporal janitor activity uses a heartbeat for liveness rather than relying on a short overall timeout that turned tombstone-machine hangs into elevated workflow-failure alerts.
