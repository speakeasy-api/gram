---
"hooks": patch
---

Collect the Codex MCP server inventory at session start, as the relay already
does for Claude. The shadow-MCP guard uses the snapshot to say why a blocked
target is not allowed — an external URL or a local stdio server, named — rather
than the generic "MCP server is not Gram-hosted", and to scope a bypass grant
to the server the call actually routes to. Best-effort: a missing or slow Codex
CLI leaves the hook unaffected, and an explicitly disabled server is left out
so it cannot vouch for a call that routed elsewhere.
