---
"server": minor
"dashboard": minor
---

Add `agent.getPlugins` management API method consumed by the Speakeasy device agent. The endpoint accepts an `email` query parameter, resolves plugin assignments for that email plus the `*` wildcard within the caller's org, and returns the assigned plugins with each MCP server's URL. Authenticates with an org-scoped API key carrying the new `agent` scope.

Adds `agent` as a selectable scope on the existing API Keys page so admins can mint these tokens from the same place every other scope is minted.

Adds `email` as a first-class principal URN type (`urn.PrincipalTypeEmail`) so admins can assign plugins by email address. Existing `user:` and `role:` URNs are unchanged; the wildcard `*` is now exported as `urn.PrincipalWildcard`.
