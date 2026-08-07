---
"server": patch
"dashboard": patch
---

Public skill share links now use your custom domain when one is verified and activated. The share page (and a raw SKILL.md download) is served at `https://<your-domain>/shared/skills/<token>`, scoped so a domain can only ever serve skills belonging to its own organization, and the dashboard copies links with the custom domain automatically.
