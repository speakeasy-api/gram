---
"server": minor
---

Scheduled assistants now summarize their conversation history after every run, so long-lived schedules no longer accumulate unbounded context that slowed responses and risked hitting model limits. Interactive assistant threads (Slack, dashboard) also compact their history earlier, keeping long conversations responsive.
