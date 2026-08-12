---
"server": patch
---

Fix employee usage pages under-reporting tokens and cost. Ingest attributes a
person's telemetry two different ways — hook events resolve the sender's email
to a Gram user id and carry both, while the rows that actually carry tokens and
cost (Claude/Codex OTEL and the Anthropic, Codex and Cursor usage imports) carry
only the provider account's email. The employee-scoped queries matched a single
collapsed identifier, so they saw one shape and silently dropped the other: an
employee page could show sessions and tool calls next to zero tokens and zero
cost.

The per-user metrics summary, observability overview, time series, tool
breakdown and data-flow graph now scope to the employee's whole identity set —
their Gram user id, their directory email, and the emails of their linked AI
accounts — resolved from the user directory rather than from telemetry row
identity, so a stray row cannot hand one person another's usage. Personal
accounts benefit most, since they usually sign in with an email that differs
from the directory one and previously joined to nothing. The per-user metrics
summary also selects cost and cache tokens, which its response has always
carried but the query never populated.
