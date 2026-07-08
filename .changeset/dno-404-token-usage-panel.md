---
"dashboard": patch
"server": patch
---

feat: add a Vercel-style token usage breakdown to the billing page's Tokens Under Management section (DNO-404). A billing-cycle picker scopes the TUM usage bar and a new "Token usage" panel to any contracted cycle; the panel renders a stacked bar chart of org-wide tokens for that cycle, sliced via a grouped, searchable breakdown picker — by any analytics dimension (project, division, department, user, agent, model, MCP server, …), by token type (input / output / cache read / cache write), or by risk involvement (tokens from sessions with at least one active risk finding, via the new org-scoped `telemetry.queryRiskTokens` endpoint) — with daily/weekly/monthly granularity and a cumulative view.
