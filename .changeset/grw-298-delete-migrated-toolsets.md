---
"server": patch
---

Deleting a toolset whose MCP server was moved to a remote MCP server now succeeds and leaves that server, its address, plugin entries and access untouched. It previously failed with "hosted MCP identity belongs to another server".
