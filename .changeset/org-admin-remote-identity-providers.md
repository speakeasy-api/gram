---
"server": minor
"dashboard": minor
---

Add an organization administrator UI for managing Remote Identity Providers
(remote session issuers), their clients, and sessions across the organization.
The `organizationRemoteSessionIssuers` management service gains an org-scoped
admin surface: a combined listing of organizational and project-specific issuers
with client counts and project names, drill-downs into each issuer's clients
(with MCP server attachment counts), each client's attached MCP servers and
sessions, authoritative delete pre-flight summaries, and write operations to
update or delete issuers and clients, detach a client from an MCP server, revoke
a single session, and revoke all of a client's sessions. Reads require `org:read`
and writes require `org:admin`; destructive actions are audited, with a bulk
revoke-all recorded as a single audit event.
