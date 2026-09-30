---
"server": patch
---

Stop PR preview workers from taking over the development environment's background schedules, which could stall jobs such as outbox publishing.
