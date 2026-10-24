---
"server": minor
---

A legacy hosted toolset's `/mcp/<slug>` alias now follows the endpoint that holds it: renaming that endpoint moves the alias, and deleting it moves the alias to the server's platform endpoint, else its oldest one. A hosted server's slug no longer follows its endpoint, so look a hosted server up by its endpoint slug, not its server slug, after an address change. Visibility and the session issuer are no longer copied onto the toolset. Name and tool variations stay toolset settings: `mcpServers.update` still writes them to the toolset, and the server mirrors them.
