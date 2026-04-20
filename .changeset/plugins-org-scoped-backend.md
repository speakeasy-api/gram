---
"server": minor
"dashboard": minor
---

Move plugins from project-scoped to org-scoped. Plugin CRUD endpoints
no longer require a project header. Toolset validation in AddPluginServer
now checks org membership instead of project, enabling cross-project
toolset references within the same organization.
