---
"hooks": patch
---

Collect the Codex MCP server inventory at session start, as the relay already
does for Claude. The shadow-MCP guard resolves a tool call's target against
that snapshot, which is what lets a Gram-hosted server be told apart from a
shadow one — without it the guard can only reach its generic "not Gram-hosted"
verdict. Best-effort: a missing or slow Codex CLI leaves the hook unaffected,
and an explicitly disabled server is left out so it cannot vouch for a call
that routed elsewhere.
