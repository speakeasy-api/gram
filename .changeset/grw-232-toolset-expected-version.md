---
"server": patch
"dashboard": patch
---

Saving an MCP server's tool list in the dashboard no longer silently overwrites a change someone else made in the meantime. Toolset reads now return a `version_token`, and `toolsets.update` accepts an optional `expected_version_token`: when it is sent and the tool list has changed since that read, the update is refused with a `conflict` error and nothing is changed. Callers that omit it keep the previous behavior. The dashboard sends the token it loaded when adding or removing tools and, on a conflict, explains that someone else changed the toolset and offers to reload.
