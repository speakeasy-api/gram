---
"server": minor
---

Staff can migrate MCP servers that still proxy an external MCP attachment from a new External MCP migration page in the admin dashboard. Each migration switches the server in place to a remote MCP server on its existing sign-in provider, so it keeps its ID, address, plugin entries and access grants, and signed-in sessions keep working. Stored header values are copied to the remote server, and headers that clients send are passed through unchanged. The old toolset is disabled, not deleted. Servers whose setup cannot carry over are listed with the reason. A preview runs every step and rolls it back first, and no sign-in provider or client is created. The page can be filtered by organization and project.
