---
"server": patch
---

Accept replayed hook events on hooks.ingest: an optional X-Gram-Replayed header marks deliveries redelivered from a device's offline spool after control-plane downtime. Replayed deliveries claim the idempotency guard for 48 hours (instead of the 10-minute retry-burst window) so competing drain triggers dedupe, and their telemetry rows carry gram.hook.replayed so dashboards can separate backdated backlog from live traffic.
