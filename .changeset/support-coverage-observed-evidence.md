---
"server": minor
"dashboard": minor
---

Back the platform-admin support coverage page with observed evidence instead of hardcoded placeholders.

Policy enforcement, identity attribution and shadow MCP exposure previously rendered as "unavailable" on every surface for every organization, because the page only ever read session and token counts and hardcoded the other three rows. They are now served from real sources through a new org-scoped `telemetry.getSupportCoverage` endpoint: policy enforcement joins `tool_call_blocks` to the session summaries via `chat_id`, identity comes from per-session user attribution, and shadow exposure comes from a new `shadow_mcp_inventory_surfaces` table that records which consuming surface reached each shadow MCP server.

Each cell now carries an explicit status, so evidence found, evidence absent, and evidence not yet reportable are three distinct states rather than one blank cell. Identity reports sessions bound to a named user separately from sessions bound only to a device hostname, which previously would have counted as attributed.

Folding a raw `hook_source` onto a surface moves from the dashboard into `internal/agentsurface`, beside the ingest that produces the values. The client-side copy silently discarded any source missing from its map, so unrecognized activity vanished from the matrix while the summary still reported full coverage; unmapped sources are now reported to the page instead.

The integration footprint cards are ranked by how much of the organization's missing coverage each one would close, and hovering a card highlights the matrix cells it reaches.

Shadow surface tagging starts at deploy and does not backfill, so shadow cells read as not-yet-reportable until tagged activity accumulates.
