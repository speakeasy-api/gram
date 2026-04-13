---
"server": patch
---

Fixed an issue where toolset lookup for install pages had fallback logic that, when a custom-domain-scoped query returned no rows (e.g. because the toolset was deleted), would retry with a slug-only query ignoring the domain. This caused the install page to serve a different org's active toolset that shared the same MCP slug instead of returning 404.
