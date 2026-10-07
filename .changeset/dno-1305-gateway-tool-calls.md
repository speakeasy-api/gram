---
"server": patch
---

Tool calls run through hosted MCP servers now also appear in agent_events, as a tool_call when the call starts and a tool_call_result when it completes, with the tool, the MCP server, the client that made the call, the session, the outcome and the duration filled in, so hosted tool use shows up next to the events agents report themselves.
