---
"server": patch
"admin": patch
---

MCP tool calls that answer HTTP 200 with a result carrying `isError: true` now count as `failed` in MCP server health, Platform MCP diagnostics and drill-down, and per-user error flags. Before this change they counted as successes. `failed` is the same class hook-observed tool errors already use.

Remote MCP proxy and playground tool-call rows now record the tool's verdict, and the proxy logs in-band tool errors at warn level, without the result body. When a server's only failures are tool errors, fault attribution reports `tool_error_dominant`; tool errors no longer dilute status-based majorities.

Hosted calls are reclassified back to 2026-10-15, when their rows began recording the verdict. Older hosted rows, and remote rows from before this deploy, still count as successes. The dashboard charts, project overview, tool usage summaries and tool-call search still classify by HTTP status only.
