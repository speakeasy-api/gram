---
"server": minor
"dashboard": patch
---

Administrators can now see and revoke the bootstrap tokens (device join tokens) that enroll headless devices. `workloadIdentities.listDeviceJoinTokens` lists an organization's tokens newest first, optionally for one agent, with who created each, when it expires, which device redeemed it, and a status of `active`, `redeemed`, `expired` or `revoked`. It requires `workload:read` and never returns the token or its hash. `workloadIdentities.revokeDeviceJoinToken` revokes a token that has not been redeemed, after which `registerDevice` refuses it; a redeemed or already revoked token is refused with a failed precondition. Revoking needs the same authority as creating: a person with `workload:write` who owns the agent or holds `agent:authorize` on it. Each revocation is recorded in the audit log, and the audit log now calls these "bootstrap tokens".

The Platform MCP exposes secret-free enrollment token metadata and confirmed revocation through the same workload permissions, agent ownership checks, and audit transaction. Token creation also returns its issuance ID for exact lifecycle tracking.
