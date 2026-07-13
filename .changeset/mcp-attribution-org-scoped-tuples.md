---
"server": patch
---

Fix MCP attribution never promoting when the Claude plugin authenticates with an org-wide hooks key. The transcript-attribution tuple was keyed in Redis by the project resolved from the plugin's `GRAM_HOOKS_PROJECT_SLUG` (default `"default"`), while the promotion worker looked it up by the staged OTEL row's project — set by the OTEL exporter's own credential. With an org-wide key the two disagree, so the join always missed and staged rows promoted verbatim as `custom` after the timeout. The tuple is now keyed by org id — both ingest paths always agree on the org, and cross-org isolation is preserved — with the row's org materialized onto `telemetry_logs_staging` as the lookup scope.
