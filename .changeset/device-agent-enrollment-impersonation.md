---
"server": patch
---

Refuse device-agent manual enrollment under impersonation: cliAuth.authorize now rejects admin org-override sessions, WorkOS user-impersonation sessions, and sessions whose user is not a member of the active organization (DNO-938).
