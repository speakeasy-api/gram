---
"server": minor
---

Connecting to an MCP server now requires an explicit `mcp:connect` grant, directly or through a role. `mcp:read` and `mcp:write` no longer imply it, so viewing or managing a server no longer lets a member call its tools. Blocks work as before: a block on a server still withdraws `mcp:connect`, and a grant made directly to a user that names the server still outranks a block inherited from a role or from everyone. Existing grants were backfilled with a matching `mcp:connect` beforehand, so no one loses access they had.

Agent credentials issued before this change keep the `mcp:connect` their stored policy records next to `mcp:read` or `mcp:write`, and still need a live `mcp:connect` grant to use it.
