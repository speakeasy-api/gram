---
"dashboard": patch
"server": patch
---

feat: show which users are running the device agent. The org Device Agent page gains an admin-only "Active Users" tab listing who has synced, attributed by the email each agent reports on its ~60s `agent.getPlugins` poll. A best-effort per-`(org, email)` last-seen record (throttled to ≤1 write/min) backs a session-secured, org-admin-gated `agent.listSyncedUsers` endpoint.
