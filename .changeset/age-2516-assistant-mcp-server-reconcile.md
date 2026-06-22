---
"server": patch
---

Assistants now pick up MCP server additions and removals on the next turn instead of only on a fresh runtime bootstrap. The per-turn dispatch sends the current MCP set to the runner, which reconciles its live connections without recycling the VM. Previously a newly attached integration (e.g. GitHub MCP) stayed invisible to the running assistant until the runtime was restarted, leaving the model unable to use it or to invoke `mcp_force_reconnect` for it.
