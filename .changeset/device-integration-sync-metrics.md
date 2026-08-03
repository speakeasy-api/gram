---
"server": patch
---

Emit OpenTelemetry metrics for the device-integration sync pipeline: `gram.device_integration.sync.outcome` (sync runs by provider and outcome) and `gram.device_integration.sync.auto_pause` (schedules auto-paused after a streak of credential rejections). These back the sync-failure-rate and auto-pause monitors for the MDM integrations rollout.
