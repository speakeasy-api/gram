---
"server": minor
---

Added `hooks.getStatus`, an organization-read endpoint that reports whether hook telemetry is configured: an active hooks-scoped API key or a connected Anthropic inference hooks integration. It reads configuration only and never hook traffic, so the dashboard can consult it on every policy edit.
