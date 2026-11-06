---
"server": minor
---

The device agent can now download the organization's Claude observability plugin for a user to upload to claude.ai, so sessions in Claude's cloud environment are recorded under that user. The plugin carries a hooks-only key owned by the user, and each download replaces their previous one. The download needs the user's own key; the shared org install key is refused.
