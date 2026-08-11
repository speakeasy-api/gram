---
"server": minor
"dashboard": minor
---

A shadow-MCP block link now redeems into the MCP approval workflow: the blocked employee's ask attaches as a requester on the server's single review — deduplicated by canonical URL, evidence gathered — instead of minting a per-user bypass request. The redemption endpoint reports what the token turned into, keeps the legacy bypass request only for identity-only servers and organizations without the approval feature, and the standalone Approval Requests review page retires — the Shadow MCP servers table is the one review surface. The command palette surfaces pending access requests in its place.
