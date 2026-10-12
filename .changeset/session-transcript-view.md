---
"server": patch
---

Agent events now record the lane each record entered through (`channel`: `otel`, `hooks`, `gateway` or `chat`), and a new `session_transcript` view folds the observations of each message and tool call within a session into one row per event, so a session can be read back as a transcript without de-duplicating the raw events first.
