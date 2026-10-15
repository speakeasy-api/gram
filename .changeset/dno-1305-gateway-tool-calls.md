---
"server": patch
---

Tool calls run through hosted MCP servers now also appear in agent_events as one tool_call per call, published as an OpenTelemetry GenAI execute_tool log after the call returns and off the response path, so hosted tool use shows up next to the events agents report themselves.
