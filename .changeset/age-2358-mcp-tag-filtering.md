---
"server": minor
---

The public `/mcp` handler now supports filtering exposed tools by variation tag via the `?tags=` URL query parameter (comma-separated, OR/union). Tool variation overrides are resolved from the MCP server's or toolset's configured tool variation group, falling back to the project default.
