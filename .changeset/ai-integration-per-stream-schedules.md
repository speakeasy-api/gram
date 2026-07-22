---
"server": minor
"dashboard": minor
---

Expose per-schedule state for AI integrations and rebuild the dashboard page around it. New aiIntegrations.listSchedules, setScheduleEnabled, and retrySchedule endpoints surface each sync schedule's status (pending/success/failed/auto-paused/disabled), last error, and timestamps, backed by a new user-controlled disabled_at pause that is independent of auto-pause. Each schedule also carries a backend-owned product-level stream identifier and kind (e.g. claude.chat.message events, cursor.usage and claude.chat.cost.usd metrics). The AI Integrations dashboard section moves to a dedicated page with one expandable row per provider connection showing its event and metric streams, each with live status, inline errors, retry, an independent pause toggle, and a link to where the imported data lands.
