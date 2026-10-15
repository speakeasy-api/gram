---
"server": minor
---

Staff can migrate MCP servers that still proxy an external MCP attachment from a new External MCP migration page in the admin dashboard. Each migration creates a remote MCP server on the server's existing sign-in provider, or merges into an existing remote server on that provider. The server keeps its address, its plugin entries, assistant attachments, metadata, gateway memberships and access grants, and signed-in sessions keep working. The old toolset is then deleted. A preview runs every step and rolls it back first, and no sign-in provider or client is created.
