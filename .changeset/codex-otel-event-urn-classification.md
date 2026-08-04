---
"server": patch
---

Classify Codex OTEL rows as provider OTel telemetry, matching Claude's. The
canonical event URN had cases for `claude-code:otel:logs` but none for
`codex:otel:logs` or `codex:otel:metrics`, so Codex's provider-native stream
fell through to the agent-hook default and was typed
`urn:telemetry:agent_hook:log:unknown` — with no event name, since those rows
carry a producer `event.name` rather than a Gram hook event. Any filter that
selects provider-OTel rows by URN prefix therefore excluded Codex while
including the equivalent Claude traffic. Codex OTEL logs now type on their
producer event name (`codex.sse_event`, ...) and metric points on their
metric name.
