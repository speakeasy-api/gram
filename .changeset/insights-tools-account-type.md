---
"dashboard": patch
---

Add a team vs personal account type filter to the MCP Servers & Tool Insights (/insights tools) page. Selecting an account type scopes the tool usage summary (served from the `trace_summaries` view, which now carries `account_type`); leaving it unset includes all traces.
