---
"server": patch
---

Remove the `telemetry-logs-pubsub-shadow` PostHog killswitch from the telemetry Pub/Sub shadow dual-write; rows written to `telemetry_logs` are now always mirrored to the `gram-telemetry-v1-log-record` topic. The flag was evaluated locally with a constant distinct ID and no groups, which cannot satisfy a group-targeted release condition — evaluation failed on every batch and the fail-closed gate meant nothing was ever published (while emitting a warn log per batch). The publish path is already best-effort and non-blocking, so the extra killswitch added more failure surface than safety.
