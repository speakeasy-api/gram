---
"server": minor
"dashboard": minor
---

Adds an org-level AI Integrations product surface with Cursor as the first provider. Organization admins can connect a Cursor Admin API key from org settings, and an hourly Temporal workflow polls Cursor for token and cost usage events and writes them into ClickHouse `telemetry_logs` so the dashboard shows Cursor usage and cost alongside Claude Code data. The dashboard cost copy is updated to reflect Cursor and Claude Code coverage, and the employee detail page now shows cost beside total tokens.
