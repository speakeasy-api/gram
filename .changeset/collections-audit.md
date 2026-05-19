---
"server": minor
---

Emit audit log entries for collection mutations: `mcp_collection:create`, `:update`, `:delete`, `:attach_server`, and `:detach_server`. Update/AttachServer/DetachServer now run in a transaction alongside the audit insert, and a new `urn.McpCollection` identifier (prefix `mcp_collection`) is used as the audit subject.
