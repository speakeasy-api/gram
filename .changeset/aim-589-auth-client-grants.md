---
"server": minor
"dashboard": patch
---

Add the `mcp:auth` and `mcp:blocked_auth` permissions for upstream auth clients. Grants can name a client, a client type, a project, or the owner of a personal connection, including `$represented_user` for the user a request acts for. Checks match strictly, never let anyone use another user's connection, and require an agent's authorizing user to hold the permission too. Agent policies can hold the new scopes, and delegated policy version 5 can encode them; credentials keep being issued at version 4 until every server reads version 5. No request is checked against the new scopes yet, and where access control is skipped, such as for legacy API keys, using an auth client is always denied.
