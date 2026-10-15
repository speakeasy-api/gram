---
"server": patch
---

Refreshing an MCP grant no longer asks for a reconnect when identity chaining serves the upstream. Such grants have no interactive upstream session, so refresh used to fail with `invalid_grant` once the access token expired. Refresh still asks for a reconnect when the MCP runtime would: the human's retained sign-in can no longer be renewed, or no identity provider for the upstream can serve. Transient failures and configuration problems a reconnect cannot fix let the refresh proceed.
