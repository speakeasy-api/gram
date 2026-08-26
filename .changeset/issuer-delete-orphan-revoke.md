---
"server": patch
---

Deleting a user session issuer (directly or through the MCP server delete cascade) now tombstones the remote sessions of any client whose only live binding belonged to that issuer, in the same transaction, and pushes best-effort RFC 7009 revocations to the upstream identity provider after commit. Previously those grants became unreachable from every binding-scoped path, were skipped by the refresh sweep, were never revoked upstream, and silently resurrected if the client was re-bound. Clients still bound to a live sibling issuer are untouched.
