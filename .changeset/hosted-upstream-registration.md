---
"server": patch
---

Hosted MCP servers use the upstream registration assigned to that server. When setup is incomplete, consent asks an administrator to assign a registration or explicitly choose No upstream authentication without changing downstream sign-in.

Removed the internal `remoteSessionClients.attachUserSessionIssuer`, `remoteSessionClients.detachUserSessionIssuer`, and `organizationUserSessionIssuers.listRemoteSessionClientBindings` RPCs. Use the server-scoped `remoteSessions.attachServerClientAssociation`, `remoteSessions.detachServerClientAssociation`, and `remoteSessions.listServerClientAssociations` operations to manage explicit placements. Those operations do not edit issuer-wide bindings.
