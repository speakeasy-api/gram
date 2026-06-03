---
"server": patch
---

Add the `platform_dashboard_send_message` egress tool so a dashboard assistant can deliver its reply to the conversation log: it resolves the target chat from the assistant principal's thread id and appends an `assistant` row to `assistant_dashboard_messages`. The user's turn is recorded as a `user` row at ingest, atomically with the thread event (idempotent on retry). Assistant-agnostic and keyed by the configurable correlation id. Foundation for AGE-2631.
