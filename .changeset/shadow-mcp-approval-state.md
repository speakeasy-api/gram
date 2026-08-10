---
"server": minor
---

Shadow MCP inventory servers now carry their MCP approval request state. Approval request summaries expose the inventory server slug for server_url targets, and inventory list/detail responses include the approval request (id, status, requester count) tracking each server, joining the two surfaces on the same canonical URL identity.
