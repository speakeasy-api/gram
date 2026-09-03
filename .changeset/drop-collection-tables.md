---
"server": patch
---

Drop the three `organization_mcp_collection*` tables and the `organization_mcp_collection_registry_id` column on `external_mcp_attachments`, now that nothing reads or writes them.
