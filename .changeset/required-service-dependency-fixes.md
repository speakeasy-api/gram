---
"server": patch
---

Stopping an assistant turn that runs in the background worker now publishes the "turn interrupted" event, so the dashboard stops streaming the turn. Plugin publication evidence now resolves from the Platform MCP instead of always reporting authorization unavailable, and plugin publishes started by the background worker now refresh the dashboard's collaborator and version caches.
