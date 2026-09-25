---
"server": minor
"dashboard": patch
---

Add Observability plugin credential rotation. Organization admins can mint a replacement hooks-scoped ingest key from the Plugins page, choosing whether the previous key is revoked immediately or kept valid for a 7-day grace window, without re-downloading the plugin or deleting keys on the Keys page. Rotation republishes the marketplace's observability plugin when the organization is eligible, and leaves consumer MCP keys untouched.
