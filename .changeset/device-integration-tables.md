---
"server": minor
---

Add the schema foundation for device integrations — the framework that connects an organization to external device-management and compliance vendors. Three new tables: `device_integration_configs` (the audited, per-org, per-provider integration identity, with secret credentials as an encrypted write-only JSON blob and non-secret settings in readable jsonb), `device_integration_syncs` (scheduler state per config and schedule, modeled on `ai_integration_syncs` including the separate auto-paused vs user-disabled markers, plus a pushed-snapshot digest so evidence sinks can skip no-op pushes), and `mdm_devices` (the MDM-reported hardware inventory, keyed by config with both the raw MDM-reported user email and a resolved `users.id`, and a `missing_since` lifecycle instead of deletes). Also adds a case-insensitive `(organization_id, LOWER(email))` index to `device_agent_syncs`, the agent-heartbeat side of the upcoming coverage join.
