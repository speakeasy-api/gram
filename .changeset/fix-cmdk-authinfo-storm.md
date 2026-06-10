---
"dashboard": patch
---

Fix a request storm of `GET /rpc/auth.info` 401s introduced by the command
palette's Recently Visited feature. The user-id lookup issued an unconditional
`auth.info` request from the always-mounted palette (including on the
unauthenticated login page). The session lookup is now gated on the palette's
open state, and the page-visit recorder reuses the session already fetched by
the auth provider instead of issuing its own request.
