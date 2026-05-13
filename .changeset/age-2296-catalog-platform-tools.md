---
"server": minor
---

Assistants can now search and install MCP catalog servers from a chat. `platform_search_catalog` queries the configured MCP registries and returns each result's registry_id + registry_specifier alongside tool previews; `platform_install_catalog_server` installs a server into the caller's project, creating a new toolset wired to the server's tools and enabling it for MCP — the same effect as the "Add to Project" dialog in the catalog UI.
