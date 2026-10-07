---
"server": patch
---

agent_events rows now carry their session, turn, subject id, user email, external user id and external organization id from the OTel transform step, so every consumer of the normalized record sees who and where an event belongs to, and a producer that stops stating one of them is counted the same day.
