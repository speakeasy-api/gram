---
"server": patch
---

Slack platform tools on a server with no Slack token, a token Slack rejects, or a token without a needed scope now fail with an error that says what is missing and who can fix it, instead of a generic internal error. A tool call the caller has to fix now answers with the tool's own client-safe message and code on every path (direct `/mcp/{slug}` calls, assistants, Platform MCP, and gateways) instead of the generic "tool call was rejected as invalid or not permitted". Rejections that carry no such message keep the generic text.
