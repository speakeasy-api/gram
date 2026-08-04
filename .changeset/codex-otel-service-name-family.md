---
"server": patch
---

Capture Codex OTEL telemetry from every client mode, not just the interactive
CLI. Codex reports a different OTEL `service.name` per mode — `codex_exec`
for headless `codex exec` (what CI and scripted runs use), plus `codex_tui`
and `codex_mcp` — but the ingest matched only `codex_cli_rs`, so every other
mode's telemetry was dropped with no rows, no token metering, and no error.
The ingest now matches the `codex_` service-name family, so a new client mode
is captured on arrival instead of being discovered later as missing data.
