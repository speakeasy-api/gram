---
"server": minor
"dashboard": minor
---

Setting up a personal sign-in for an MCP server now always creates a sign-in of the server's own. The dashboard offers **New from scratch** or **New from existing** in place of reusing a stored one: starting from an existing sign-in copies its settings for review into an independent sign-in, and the stored client secret is copied on the server without being shown. This stops a member's sign-in for one server from replacing the access another server obtained through a shared sign-in. Service accounts and stored or caller-supplied credentials can still be shared between servers, and existing shared sign-ins are left as they are.
