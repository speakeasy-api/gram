---
"server": minor
"dashboard": patch
---

Agent identity pages no longer send one MCP server activity request per project. A new `telemetry.getOrgMcpServerActivity` endpoint returns recent activity for every readable project in one request and scans only the recent window. The agents roster also stops loading people-only data it never shows.
