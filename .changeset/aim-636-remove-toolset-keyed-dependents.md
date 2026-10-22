---
"server": patch
---

Stop reading and writing toolset-keyed hosted MCP dependents. Plugin membership, MCP metadata and assistant attachments use only the MCP server key, so `plugin_servers.toolset_id`, `mcp_metadata.toolset_id` and `assistant_toolsets` can be dropped. The hosted MCP wrappers migration tool keeps only its wrapper backfill; its `-move-dependents`, `-cleanup-orphans` and `-rekey-removal-records` modes are removed. Role delivery no longer adds a hosted server whose toolset has been deleted.
