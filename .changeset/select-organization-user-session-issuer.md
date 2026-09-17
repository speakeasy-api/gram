---
"server": minor
"dashboard": minor
---

MCP servers can select an existing organization- or project-owned user session issuer during creation or from authentication settings. Interactive creation prefers a sole organization issuer, requires a choice when several exist, and keeps project-specific issuer creation as an explicit fallback. Organization-owned issuer settings remain read-only from project pages.
