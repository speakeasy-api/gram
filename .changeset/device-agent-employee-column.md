---
"dashboard": patch
---

feat(observe): show device agent status on the Employee Enrollment table. A new admin-only "Device Agent" column surfaces whether each member's Speakeasy device agent has checked in — Active (synced recently), Stale (enrolled but not seen lately), or Not Enrolled (no agent activity) — attributed by email via the org-scoped `agent.listSyncedUsers` endpoint. The column only appears when the `gram-device-agent` feature is enabled, self-refreshes on a 30s tick, and is sortable so admins can surface who is (or isn't) running the agent alongside their telemetry enrollment.
