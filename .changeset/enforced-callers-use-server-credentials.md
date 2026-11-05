---
"server": patch
---

In an organization that enforces upstream auth, every caller of a remote or tunneled MCP server now gets its upstream credential from the credentials that server offers, never from whatever the server's sign-in happens to link. People whose chosen credential had outdated header copies, could fall back to company sign-in, or was an identity-provider credential were asked to reconnect, or refused a token refresh, when the sign-in did not also link that credential; they now keep working. Agent API keys with no credential picked for a server, assistants without an agent identity, and Platform MCP calls through the organization gateway use the one credential the server offers them, or none when it needs none, and are refused when there are several to choose from. An assistant without an agent identity can no longer use a person's own connection; give it an agent identity to act for that person. A session for a hosted server can refresh its token again.
