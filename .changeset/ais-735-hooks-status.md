---
"server": minor
---

Added `hooks.getStatus`, a project-read endpoint that reports whether hook telemetry is configured for the project: an active hooks-scoped API key bound to the project or organization-wide, or an enabled Anthropic inference hooks integration for the project. It reads configuration only and never hook traffic, so the dashboard can consult it on every policy edit.
