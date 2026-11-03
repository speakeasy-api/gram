---
"server": patch
---

Slack trigger webhooks now treat the URL verification handshake as a challenge-only response. A request that skips signature verification as a handshake is never ingested as an event, and the handshake is only recognized on `application/json` requests. Slack webhook requests whose `Content-Type` is neither `application/json` nor `application/x-www-form-urlencoded` are rejected.
