---
"server": minor
---

Add management APIs for user sessions:

- **userSessionIssuers**: configure the authorization servers that mint user sessions for your MCP servers.
- **userSessionClients**: inspect and revoke the OAuth clients that have dynamically registered against those issuers.
- **userSessions**: list the sessions minted for end users and revoke any that should no longer be honored.
- **userSessionConsents**: list and withdraw the consent records that gate which (subject, client) pairs skip the consent prompt.
