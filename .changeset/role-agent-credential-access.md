---
"server": minor
"dashboard": minor
---

Roles and agents can now be limited to particular upstream credentials. The role editor and an agent's Permissions page show every credential by kind, with a tick on all credentials, one kind, or one credential, and a block that wins over any tick. A wide tick can be split into one per kind or per credential so one can be taken away while the rest stay, and unticking a credential that comes from a wider tick splits it for you. The role editor's MCP server grid shows how many of each server's credentials the role can use and flags servers it can connect to but not sign in to, and a server's Check access shows which credentials a member can sign in with and which rule decides each one.
