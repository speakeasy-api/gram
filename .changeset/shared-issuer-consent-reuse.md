---
"server": minor
---

Speakeasy remembers per-server approvals for identified users connecting through a shared authorization server. Unchanged access can reuse the saved tool selection and session length; new servers, changed tool inventories, and missing upstream permissions require consent. Withdrawing consent forgets every approval for that person and client under the issuer without revoking existing sessions.
