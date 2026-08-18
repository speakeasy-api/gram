---
"dashboard": patch
---

Remote identity providers listed on an MCP server's Authentication settings now link through to their detail page. The rows were inert text, so reaching a provider's detail page meant navigating to Remote Identity Providers separately and finding it again by hand. Providers of every tenancy tier resolve through the same tenant-scoped detail page, which already renders an inherited platform provider read-only, and the name falls back to plain text for a viewer without organization read access.
