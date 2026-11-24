---
"server": minor
"dashboard": patch
---

Adds the `mcpConnectionHealth` API behind the `mcp-connection-health` flag: Connection Logs for one MCP authentication provider (a paged list with repeats folded into one row, totals and a count per filter value, one attempt's steps, and lookup by reference), the sign-in and renewal summary, active sessions per day with the average session length, and the apps refused at sign-in. Members without permission to read logs see only their own attempts, and the reads say when the organization has logging off.
