---
"server": patch
---

Refreshing an MCP grant no longer asks for a reconnect when identity chaining serves the upstream. Such grants have no interactive upstream session, so refresh used to fail with `invalid_grant` once the access token expired. Refresh still asks for a reconnect when chaining does not serve the upstream, or when stored state confirms that a fresh sign-in repairs it: the human's retained sign-in can no longer be renewed, or it predates the current identity provider registration. Anything refresh cannot confirm lets it proceed: failed reads, an upstream whose discovery is not cached, and configuration problems that only an administrator can fix.
