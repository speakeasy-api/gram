---
"server": patch
---

OTLP exports accepted on `/otel/v1/logs` and `/otel/v1/metrics` now also run the hooks telemetry writers, so Claude Code and Codex usage exported to the native ingest endpoint is attributed to users and counted on usage, cost, and identity pages. Previously those exports only reached the Event Feed.
