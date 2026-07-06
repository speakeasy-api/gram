---
"server": minor
---

Add tool metadata management methods to the mcpServers service: setToolMetadataBatch (authoritative transactional batch upsert that retires stored tools absent from the payload), listToolMetadata, setToolMetadata, and deleteToolMetadata. Mutations require mcp:write, are scoped to the target MCP server, and record one collection-level audit entry per write; reads require mcp:read.
