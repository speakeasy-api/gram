---
"server": patch
---

Isolate Claude Code session identity per `session.id` when an OpenTelemetry Collector or gateway re-batches multiple sessions into one OTLP logs export, so a session is never cached or authorized with another session's `user.email` / `organization.id`.
