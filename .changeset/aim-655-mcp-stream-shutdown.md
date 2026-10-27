---
"server": patch
---

Ends relayed MCP listen streams as soon as a server starts shutting down, so clients reconnect to a replica that is still serving instead of holding shutdown open for the full drain window. Standalone GET streams close with an SSE retry field and 2026-07-28 `subscriptions/listen` streams close with their completion result. Requests still running at the shutdown deadline are now cancelled and their connections closed right away, and relayed streams record why they ended, whether their response had been relayed, and whether they were resumable.
