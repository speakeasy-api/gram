---
"server": patch
---

Keep a hosted toolset's own MCP server and the gateway members on the same toolset apart. Platform MCP diagnostics give the toolset slug and its plugin entries to the canonical server without counting member traffic. Role delivery adds the canonical server once, and keeps entries it already delivered. Adding the canonical server as a gateway member is now rejected.
