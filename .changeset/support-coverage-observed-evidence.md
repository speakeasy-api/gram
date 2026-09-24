---
"server": minor
"admin": minor
---

Add an observed support coverage matrix to the staff admin app, on each organization's record.

A new `admin.getSupportCoverage` endpoint reports, per consuming surface, evidence for session activity, policy enforcement, identity attribution, token usage and shadow MCP exposure. Policy enforcement joins `tool_call_blocks` to session summaries via `chat_id`; identity counts sessions bound to a named user separately from those bound only to a device hostname; shadow exposure is derived from `trace_summaries`, which already carries the MCP server URL and hook_source on the same trace.

Every `(capability, surface)` pair is returned with an explicit status so evidence found and evidence absent stay distinct, and hook sources the fold does not recognize are reported rather than dropped. Folding a raw `hook_source` onto a surface now happens server-side in `internal/agentsurface`.

The integration cards beside the matrix read from the operator-editable support matrix catalog, ranked by how much of that organization's missing coverage each method would close.
