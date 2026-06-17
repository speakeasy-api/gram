---
"server": minor
"dashboard": minor
---

Add an organization administrator "Refresh now" action for remote sessions. The
`organizationRemoteSessionIssuers` management service gains a `refreshSession`
method that forces an upstream `grant_type=refresh_token` exchange on a single
session regardless of its current access-token expiry, persists the rotated
tokens, and returns the updated session. The shared refresh code path is now
used by both the lazy MCP token-resolution path and this explicit admin action;
the upstream token POST runs outside any database transaction. The
`RemoteSession` type exposes a `has_refresh_token` flag (the encrypted token
itself stays unexposed) so the dashboard Sessions tab can offer "Refresh now"
only for sessions that can actually be refreshed. Operator-actionable refresh
failures (an upstream rejection of the refresh token, an unreadable stored
token, a missing token endpoint) surface as a bad-request with a clear "Unable
to refresh: ..." reason and each refresh is recorded as a
`remote-session:refresh` audit event.
