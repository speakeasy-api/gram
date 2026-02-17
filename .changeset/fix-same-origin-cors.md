---
"server": patch
---

Fix same-origin requests failing with "Origin does not match audience claim" error in chat sessions CORS middleware.

Browsers don't send Origin headers for same-origin GET/HEAD requests. The middleware now validates the Host header against audience claims when Origin is absent, allowing legitimate same-origin requests while still preventing cross-origin bypass attacks.
