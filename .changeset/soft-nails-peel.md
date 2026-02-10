---
"dashboard": patch
"@gram/client": patch
"server": patch
"@gram-ai/elements": patch
---

Support for [MCP tool annotations](https://modelcontextprotocol.io/legacy/concepts/tools#tool-annotations). Tool annotations provide additional metadata about a tool’s behavior, 
helping clients understand how to present and manage tools. These annotations are hints that describe the nature and impact of a tool, but should not be relied upon for security decisions.

The MCP specification defines the following annotations for tools that Gram now supports for external mcp servers sourced from the Catalog as well as HTTP based tools.

| Annotation | Type | Default | Description |
| --- | --- | --- | --- |
| `title` | string | - | A human-readable title for the tool, useful for UI display |
| `readOnlyHint` | boolean | false | If true, indicates the tool does not modify its environment |
| `destructiveHint` | boolean | true | If true, the tool may perform destructive updates (only meaningful when `readOnlyHint` is false) |
| `idempotentHint` | boolean | false | If true, calling the tool repeatedly with the same arguments has no additional effect (only meaningful when `readOnlyHint` is false) |
| `openWorldHint` | boolean | true | If true, the tool may interact with an "open world" of external entities |

Tool annotations can be edited in the playground or in the tools tab of a specific MCP server.
