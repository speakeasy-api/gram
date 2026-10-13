---
"server": patch
"dashboard": patch
---

Plugin publishing now reads hosted MCP plugin entries through one path, whether an entry is keyed by its toolset or by the toolset's own MCP server. Entries keep their published URLs under either key. A hosted MCP server keyed by its server now also carries the toolset's environment headers. A plugin can no longer hold the same hosted MCP under both keys. Renaming a toolset now renames its plugin entries, and deleting a toolset now removes them. The dashboard shows a hosted MCP's plugin entries under either key.
