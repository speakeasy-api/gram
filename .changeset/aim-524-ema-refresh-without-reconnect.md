---
"server": patch
---

Refreshing an MCP grant no longer asks for a reconnect when identity chaining serves the upstream. Such grants have no interactive upstream session, so refresh used to fail with `invalid_grant` once the access token expired.
