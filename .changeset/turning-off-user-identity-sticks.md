---
"server": patch
"dashboard": patch
---

Turning off User Identity on an MCP server now takes effect in every case, including when a gateway shares its issuer or no server has claimed its sign-in client. Settings now show the sign-in clients a server's callers actually need, so the page no longer reads No Identity while requests still ask callers to sign in, and saving reports an error rather than success when nothing was removed.
