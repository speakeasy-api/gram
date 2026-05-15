---
"server": patch
---

Make hook routes (Claude / Cursor / Codex / OTEL Logs / OTEL Metrics) filterable in Datadog by `gram.org.id`, `gram.project.id`, `gram.hook.source`, and `gram.hook.event`. Replace nested `value` payloads with top-level slog attrs attached via `slog.With`, and log on every early-return path — including unauthorized requests and missing-session-id branches — so a silent 401 or no-session request is still visible when debugging hook setup for a given org/project.
