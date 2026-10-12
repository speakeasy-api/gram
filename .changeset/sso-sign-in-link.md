---
"server": minor
"dashboard": patch
---

Record the SSO sign-in behind each MCP grant and check, before Speakeasy vouches for a user to another service, that the grant still traces to a current SSO sign-in: not revoked, within seven days of the first sign-in on that WorkOS session, still a member, not deprovisioned, on one of the organization's verified domains. Sign-ins are recorded only in the organization the user signed in to, and a grant is never minted from a sign-in an administrator, sign-out everywhere or deprovisioning revoked. `auth.logoutEverywhere` ends every dashboard and identity provider session and revokes the user's sign-ins and the grants made from them; `organizations.revokeMemberSignIns` does the same for a member of the active organization. Existing grants keep working and are simply not linked.
