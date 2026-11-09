---
"server": patch
---

Tool Logs keeps the MCP server of an OpenClaw MCP call in the default view. OpenClaw names the server only on the result of a call, and the trace summary could keep the empty value from the start of the call, so the call showed as a local tool. Calls recorded before this fix keep their stored classification.
