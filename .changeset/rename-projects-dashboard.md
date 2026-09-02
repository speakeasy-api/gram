---
"server": minor
"dashboard": minor
---

Project settings now show the project's display name and slug, and project admins can update the display name.

The new session-authenticated `projects.update` endpoint validates and audits display-name changes, and the dashboard updates its project cache after a successful rename.
