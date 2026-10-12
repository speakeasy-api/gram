---
"server": patch
---

Add `-move-dependents` to the hosted MCP wrapper backfill. It re-keys toolset-keyed MCP metadata and plugin members onto each toolset's canonical MCP server in place, and moves assistant toolset attachments to assistant MCP server attachments under the same id. When a server-keyed twin exists, the toolset-keyed row wins: the twin is removed in the same transaction and its full contents are recorded for rollback. Rows whose toolset or project is deleted (`orphaned`) and assistant attachments the assistant runtime does not dial are reported instead of written. Applying it requires `-report` and `-readers-cut-over`, which confirms AIM-16 phase A (dual-key readers) and phase B (writers switched to the server key) are both fully deployed, and writes a journal entry per row, synced before each commit.

Every `-apply` now refuses an existing report path. The backfill stops on transient database errors instead of reporting them as Sync refusals, and records its audit entries without publishing audit webhook events. Audit loggers accept `audit.WithoutOutbox()` for operator backfills.
