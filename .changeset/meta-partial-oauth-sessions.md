---
"server": minor
"dashboard": patch
---

Meta MCP gateway sessions now serve with a subset of providers connected: an unconnected provider degrades only its own members (anonymous upstream call, member-scoped errors) instead of rejecting the whole session with a 401. Direct MCP endpoints keep the all-or-nothing re-auth challenge.
