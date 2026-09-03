---
"dashboard": patch
"server": major
---

Stop reading and writing the collection registry link on external MCP attachments. `organization_mcp_collection_registry_id` no longer appears on deployment or catalog responses, is no longer accepted when adding an external MCP server, and is no longer copied when a deployment is cloned. Attachments that were published from a collection kept a null registry id; they still describe, but can no longer be redeployed. The column itself is dropped in a follow-up migration.
