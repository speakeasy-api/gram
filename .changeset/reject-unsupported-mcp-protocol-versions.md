---
"server": patch
---

Reject unsupported MCP protocol versions before authentication with error code `-32022`, including the requested and supported versions so clients can retry with a compatible version.
