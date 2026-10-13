---
name: manage-mcp-sign-in-scopes
description: Inspect and change requested sign-in scopes for one exact remote MCP server in an explicit Speakeasy AI Control Plane project, with confirmed shared-server impact and live verification.
---

# Manage MCP sign-in scopes

Use this workflow when a user wants to inspect, pin, or clear the permissions
requested during sign-in to a remote MCP server. A scope pin is a request to
the identity provider, not a ceiling on granted access. Client settings and
authorization challenges can take precedence.

1. Successfully call `list_projects` through this client's authenticated
   Speakeasy connection. Package installation or another client's login does
   not grant access. Ask the user to select an exact project when ambiguous.
2. Use `find_mcp` and `get_mcp` to identify one exact remote-backed MCP server
   in that project. Do not infer a target from another project or change a
   similarly named server. Read the configured server before proposing a write.
3. Call `get_mcp_scopes` with that project's ID and the MCP server's ID. Explain
   the current pin, whether protected-resource discovery is enabled, each
   client's effective requested scopes, and `shared_server_count` (the number
   of **other** live MCP servers affected by the same pin). Unknown advertised
   scopes are not an empty or unrestricted grant.
4. Ask for the desired exact scope list, or an empty list to clear the pin.
   Call `set_mcp_scope_pin` with `confirmed: false`, the desired `scopes`, and
   `expected_scopes` and `expected_version` from the fresh read's `pinned_scopes`
   and `confirmation_version`. Show the returned proposal and the
   number of other affected MCP servers. Obtain explicit confirmation of both.
   Do not treat a broad setup request as permission to broaden scopes.
5. Repeat the same call with `confirmed: true` and the preview's current
   `pinned_scopes` as `expected_scopes` and `confirmation_version` as
   `expected_version`. This opaque version guards the resource URL and affected
   MCP membership, even when the count stays the same. A concurrent change is refused; read
   again and obtain a new confirmation instead of silently overwriting it.
   If discovery is unavailable, nonempty pins are refused but clearing an old
   pin is still allowed with write access. If permission is refused, stop and
   explain the returned limitation. Do not substitute database writes, raw HTTP, API keys,
   or another identity.
6. Call `get_mcp_scopes` again. Compare the saved pin and effective client
   requests with the proposal. If the pin does not decide a client's request,
   identify the returned precedence source instead of claiming the pin limits
   that client's access. A saved result with `verified: false` requires this
   fresh read before reporting success.

Never request or expose OAuth tokens, client secrets, credentials, or sensitive
headers. A scope configuration change is not proof that sign-in succeeds or
that the identity provider granted those scopes. Report the project, MCP
server, saved pin, shared-server impact, and what was actually verified.
