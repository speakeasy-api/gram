---
"server": minor
"dashboard": patch
---

When upstream auth enforcement refuses a call because the caller may not use a connection, the refusal now shows under failed authorization checks on the agent's or person's Permissions tab and in Roles & Permissions. A personal connection used by anyone other than its owner is listed with the reason "This connection belongs to another member and works only for them." The record names the server and the connection, never the connection's owner or upstream account.
