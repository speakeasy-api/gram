---
"server": patch
---

The remote MCP proxy now labels relayed list and `resources/read` results `cacheScope: "private"` whenever Gram's access gate or filters shape them, so a shared cache cannot serve them to another caller. Anonymous callers of public servers still receive the upstream's own cache hints.
