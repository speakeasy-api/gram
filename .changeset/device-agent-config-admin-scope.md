---
"server": patch
"dashboard": patch
---

Scope device agent fleet configuration to organization admins. Viewing it
(`agent.getConfiguration`) now requires `org:admin`, matching the existing
requirement on `agent.updateConfiguration`, and the dashboard hides the Device
Agent Configuration tab from non-admins. The Setup tab stays available to
organization readers.
