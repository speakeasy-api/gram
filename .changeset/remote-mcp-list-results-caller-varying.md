---
"server": patch
---

The remote MCP proxy now labels every relayed `tools/list` and `resources/list` result `cacheScope: "private"` with `ttlMs: 0`, on public and private servers alike, overwriting any upstream values. Proxied lists can vary by caller through header pass-through, caller OAuth tokens, and caller assertions, so an unlabelled result (read as `"public"` under MCP 2026-07-28) could be served from a shared cache to another caller.
