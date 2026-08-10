---
"hooks": patch
---

Collect the Codex MCP server inventory at session start, as the relay already
does for Claude. The shadow-MCP guard resolves a tool call's target against
that snapshot, which is what lets a Gram-hosted server be told apart from a
shadow one — without it the guard can only reach its generic "not Gram-hosted"
verdict. The list is taken from the session's working directory, since Codex
resolves its project config layer relative to it. Best-effort: a missing or
slow Codex CLI leaves the hook unaffected, and an explicitly disabled server is
left out so it cannot vouch for a call that routed elsewhere.

Hook events now also report the relay's version. The server reads its presence
as "this client can report MCP inventory" and holds back the Codex meta-tool
guard for clients that cannot, so enforcement arrives with the upgrade rather
than depending on a server deploy and a hooks release landing in the right
order.
