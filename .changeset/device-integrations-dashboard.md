---
"dashboard": minor
---

Add the Device Integrations dashboard: an org-level catalog page listing MDM
and compliance providers from the backend registry, with a credential sheet
rendered from each provider's field spec (secret values masked and
write-only), a save → test-connection → enable flow, and per-schedule sync
status with pause/resume and sync-now controls. The Device Agent page gains a
coverage section joining MDM-managed devices against agent heartbeats, with
per-bucket summary tiles and a filterable device list that visually
distinguishes the stale-agent drift case. Both surfaces are gated behind the
`gram-device-integrations` PostHog flag.
