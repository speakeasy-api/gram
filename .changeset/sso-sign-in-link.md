---
"server": minor
"dashboard": patch
---

Record the SSO sign-in behind each MCP grant and check, before Speakeasy vouches for a user to another service, that the grant still traces to a current SSO sign-in: not revoked, within seven days, still a member, not deprovisioned, on one of the organization's verified domains. Adds "sign out everywhere" and admin revocation of a member's sign-ins; deprovisioning and sign-out revoke them too. Existing grants keep working and are simply not linked.
