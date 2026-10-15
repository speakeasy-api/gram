---
"server": minor
---

Each MCP server now keeps its own upstream authorization server. Enabling User Identity on one server no longer re-points the other servers that share its organization sign-in, and a second OAuth-backed server no longer leaves every one of them reporting that no sign-in is needed while failing upstream. Signing in to a server also no longer requires connecting the providers its sibling servers use, and a server with no sign-in configured now calls its upstream anonymously instead of borrowing a sibling's credential. The sign-in page a server shows offers the connections that server signs its users in to, so people are no longer asked to connect, verify, or disconnect services that belong to another MCP server.

Turning User Identity off is now a single operation on one server (`remoteSessions.disableServerIdentityConfiguration`): it stops that server signing anyone in, and releases the provider's OAuth client only when no other server still signs in through it. Replacing a client that another server signs in through is refused with the Remote Identity Provider settings named as the place to do it, since that is the surface authorized for every server using it.

A server's upstream authorization server is now only ever what was configured on it. Speakeasy no longer assumes that a lone MCP server signs in to whichever provider its organization sign-in happens to bind, so a server whose sign-in was never set up stays without one until it is configured, and User Identity can now be configured on servers of every kind rather than only externally hosted ones.
