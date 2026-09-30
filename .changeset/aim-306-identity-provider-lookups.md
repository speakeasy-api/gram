---
"dashboard": patch
"server": minor
---

MCP server settings no longer show an attached identity provider as missing when the platform catalog is large. The identity provider picker now searches on the server and loads more on demand, and the issuer listing accepts `search` and `upstream_host` filters.
