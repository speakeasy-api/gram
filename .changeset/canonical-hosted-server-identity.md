---
"server": patch
---

Keep a hosted toolset's own MCP server and the gateway members on the same toolset apart. Platform MCP diagnostics and hosted overview metrics exclude member traffic from the canonical server, which also owns the toolset's plugin entries. Role delivery adds the canonical server once, falling back to one addressable alternate only when the canonical server is enabled but has no live endpoint, and keeps entries it already delivered. Adding the canonical server as a gateway member is now rejected.
