---
"server": minor
"dashboard": minor
---

Add a `source` filter to `chat.listChatsWithResolutions` and surface it as a "Source" dropdown next to the resolution status filter on the Agent Sessions page. Filters by the most recent non-null `source` recorded on chat messages (e.g. `claude-code`, `dashboard-ai-insights`, `playground`, `elements`), so callers can scope the feed to a single agent client.
