---
"dashboard": minor
"server": major
---

Remove MCP collections. The `collections` service and its `/rpc/collections.*` endpoints are gone, along with the Collections pages and sidebar entry, the Publishing section on MCP server settings, the collection group panel in access grant rules, the "Catalog kind" filter on Sources, and the Collection origin label on catalog sources. Collection audit actions are no longer recorded or rendered. The backing tables and the `organization_mcp_collection_registry_id` column are dropped in a follow-up migration.
