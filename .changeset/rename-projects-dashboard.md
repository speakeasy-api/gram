---
"server": minor
"dashboard": minor
---

Project admins can now update a project's display name and non-default URL slug from project settings. Slug changes require confirmation because saved links, CLI profiles, and integrations may need to be updated, while the default project slug remains protected.

The new session-authenticated `projects.update` endpoint validates and audits each change, and the dashboard updates its project cache before navigating to a renamed slug.
