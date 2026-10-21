---
"server": patch
"admin": patch
---

Record a typed auth client for every upstream OAuth client an MCP server uses, as a separate step of the admin upstream OAuth migration, retire backfilled records whose association or client is gone, and mark servers that offer one as needing upstream authentication where that is still undecided. Nothing reads these records yet.
