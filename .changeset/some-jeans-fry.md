---
"dashboard": minor
---

Introduced a diff viewer that highlights the changes in audit subjects for update events.

This establishes a baseline UX for understanding the changes happening in orgs/projects. In future iterations, some of the changes will be promoted to natural language bullet points under each audit log message.

Additionally this change adds a preprocessing step to rename toolset:_ audit events to mcp:_ since "toolsets" are no longer a visible primitive on the dashboard.
