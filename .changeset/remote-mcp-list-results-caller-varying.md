---
"server": patch
---

The remote MCP proxy now labels relayed `tools/list`, `resources/list`, `resources/templates/list`, `prompts/list`, and `resources/read` results `cacheScope: "private"` whenever Gram's own access gate or filters shape them: private servers, authenticated callers, upstream credentials or caller assertions Gram forwards, configured pass-through headers, and session tool selections. Under MCP 2026-07-28 an absent `cacheScope` reads as `"public"`, which would let a shared cache serve such a result to another caller. The upstream's `ttlMs` is kept (or set to `0` when absent) unless a Gram filter is attached to that list, in which case it is `0`. Anonymous callers of public servers with none of those inputs, and requests declaring a protocol revision older than 2026-07-28, relay the upstream's own cache hints untouched.
