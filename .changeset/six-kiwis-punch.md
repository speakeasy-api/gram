---
"server": minor
---

Added granular webhook event types for audit log entries — each auditable subject (deployments, projects, MCP servers, API keys, toolsets, risk policies, sessions, and more) now emits its own typed webhook event (e.g. audit_log.deployment_event_v1), enabling subscribers to filter by subject domain rather than receiving all audit activity under a single event type.
