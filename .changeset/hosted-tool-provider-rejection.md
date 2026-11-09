---
"server": patch
---

When a hosted server's tool call is refused by the service with 401 or 403, connection health now records it, the same way it does for other servers. The record says whose credential was refused (the person's sign-in, the organization's shared sign-in, or a saved key) and who needs to fix it. What the client receives is unchanged.
