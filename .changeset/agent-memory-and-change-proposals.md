---
"server": minor
"dashboard": minor
"@gram-ai/elements": minor
"@gram/client": minor
---

POC: Agentic AI Insights — agent memory and change proposals.

Adds an agentic surface to the dashboard's AI Insights sidebar that lets the
chat agent investigate logs, persist workspace memory across sessions, and
propose edits to tool variations and toolsets that humans review and apply.

- New `insights` backend service with 11 `/rpc/insights.*` endpoints (propose
  / list / apply / rollback / dismiss for proposals; remember / forget /
  list / record_finding for memory). Apply and rollback mutate the underlying
  variations or toolsets via the existing services with live-read drift
  detection (409 + `superseded`). Three new authz scopes
  (`insights:propose / read / apply`) and four audit action types.
- New built-in MCP server at `/mcp/ai-insights` exposing six agent-facing
  tools (the propose / remember / recall / record_finding endpoints). Apply
  / rollback / dismiss are intentionally human-only.
- `@gram-ai/elements` config `mcp` field broadens to `ServerUrl |
ServerUrl[]` so a single chat session can mount multiple MCP servers in
  parallel — used by the dashboard to mount the new ai-insights MCP
  alongside the existing observability one.
- Dashboard sidebar adds a `ProposalsPanel` (pending + history tabs, diff
  renderer, drift modal, friendly cache-stale errors), a `MemoryPill` (chip
  strip + popover), a Refresh Session button, 10s polling on list queries,
  and a system prompt augmented with a memory slice + investigation
  protocol.
- The chat completion stream now writes an OpenAI-compatible error frame to
  the SSE before closing on upstream failure, so client surfaces the real
  cause (quota / rate-limit / model rejection) instead of an empty
  `AI_RetryError`.
