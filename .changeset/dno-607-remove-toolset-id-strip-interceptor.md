---
"server": patch
---

Remove the deferred `x-gram-toolset-id` strip interceptor from the remote MCP proxy (`/x/mcp` and remote/tunneled `/mcp`), the follow-up to DNO-603. Once clients can no longer be holding a `tools/list` schema captured before DNO-603 stopped injecting the property, the strip is no longer needed and `tools/call` arguments reach the upstream server byte-for-byte as the caller sent them. The shared `StripToolsetIDProperty` helper and `XGramToolsetIDField` const remain because the toolset-backed `/mcp` path still injects them (tracked by DNO-599).
