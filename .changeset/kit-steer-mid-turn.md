---
"server": minor
---

Messages sent while a Kit-backed assistant turn is running now steer the active turn immediately via ACP v2 `delivery: "steer"` instead of waiting for the turn to settle before starting a new one.
