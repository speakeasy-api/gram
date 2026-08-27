---
"server": patch
---

Tool call metrics now cover external MCP and prompt tools, which previously went unmeasured, so usage and failure reporting account for every kind of tool an MCP server can serve.

External MCP calls are recorded against the upstream tool that ran, and a failure the upstream reports inside its result, such as an expired credential returned as tool output rather than as an error status, now counts as a failure rather than a success.
