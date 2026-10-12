---
"server": patch
---

Add `-move-dependents` to the hosted MCP wrapper backfill. It re-keys toolset-keyed MCP metadata and plugin members onto each toolset's canonical MCP server in place, and moves assistant toolset attachments to assistant MCP server attachments under the same id. Twins, rows whose toolset or project is deleted (`orphaned`), and assistant attachments the write path would reject are reported instead of written. Applying it requires `-report` and `-readers-cut-over`, which confirms AIM-16 phase A (dual-key readers) and phase B (writers switched to the server key) are both fully deployed.

The backfill now stops on transient database errors instead of reporting them as Sync refusals, and records its audit entries without publishing audit webhook events. Audit loggers accept `audit.WithoutOutbox()` for operator backfills.
