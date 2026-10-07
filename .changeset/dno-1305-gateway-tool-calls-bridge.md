---
"server": patch
---

Tool calls run through the Gram gateway now land in agent_events as tool_call_result rows, with the tool, the MCP server, the client that made the call, the session, the outcome and the duration filled, so hosted tool use shows up next to the events agents report themselves.
