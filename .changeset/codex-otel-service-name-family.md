---
"server": patch
---

Route Codex OTEL telemetry from every client mode to the Codex stream, not
just the interactive CLI. Codex reports a different OTEL `service.name` per
mode and does not use one separator convention — `codex_exec` for headless
`codex exec` (what CI and scripted runs use), `codex_tui`, `codex_mcp`, and
`codex-app-server` for Codex mode in the unified ChatGPT desktop app — but
the ingest matched only `codex_cli_rs`. Those payloads were not dropped: they fell through to the
Claude path and were persisted as `claude-code:otel:logs` rows carrying
Claude's hook source and account attribution, so Codex traffic silently
inflated Claude surfaces while never being metered as Codex usage. The
ingest now matches the whole Codex service-name family, both separators
included.

Routing is also now per OTEL resource rather than per payload: a collector
that fans several clients into one export previously had the whole batch
routed by whichever client matched first, mislabeling the other client's
records.
