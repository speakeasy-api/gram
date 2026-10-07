---
"server": patch
"dashboard": patch
---

Adding or removing tools on an MCP server in the dashboard no longer silently overwrites a change someone else made in the meantime. Toolset reads now return a `version_token`, and `toolsets.update` accepts an optional `expected_version_token`. When it is sent and the toolset's tools or resources have changed since that read, the update is refused with a `conflict` error that names the stale token, and nothing is changed. Callers that omit it keep the previous behavior. The dashboard sends the token it loaded and, on a conflict, explains that someone else changed the toolset and offers to reload.
