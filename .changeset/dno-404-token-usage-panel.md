---
"dashboard": patch
"server": patch
---

feat: add a Vercel-style token usage breakdown to the billing page's Tokens Under Management section (DNO-404). A billing-cycle picker scopes the TUM usage card and a new "Token usage" panel to any contracted cycle; the panel renders a stacked bar chart of org-wide tokens for that cycle, sliced via a grouped, searchable breakdown picker — total, by token type (input / output / cache read / cache write), by risk involvement (tokens from sessions with at least one active risk finding, via the new org-scoped `telemetry.queryRiskTokens` endpoint), or by analytics dimensions — with daily/weekly/monthly granularity and a cumulative view. Beneath the chart, a usage details table lists per-metric cycle totals with sparklines: token types, agent sessions, tool calls, and message-level stats (tokens in messages with risk findings and tokens from tool-call messages, via the new `telemetry.queryMessageTokenStats` endpoint reading Postgres per-message token counts).
