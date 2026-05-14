---
"server": minor
---

Assistants can now search and install MCP catalog servers from a chat. `platform_search_catalog` queries the configured MCP registries and returns each result's registry_id + registry_specifier alongside tool and remote previews; `platform_install_catalog_server` registers the selected catalog server as a remote MCP server in the caller's project, resolving the upstream URL (with variable substitution) and required headers from the catalog entry.
