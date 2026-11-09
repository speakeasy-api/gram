---
"server": minor
"dashboard": minor
---

Add a new "MCP connection logs" data export. When enabled, failed, blocked, and declined MCP connection attempts are sent to the project's OTEL destination with what happened, what to do, and the attempt id. Rollout is gated per organization.
