---
"dashboard": patch
"server": patch
---

A new Shadow MCP blocking policy now starts on "Allow all servers", so you block specific servers instead of every server for the whole organization by default. "Block all servers" is still available. The Platform MCP `create_risk_policy` tool takes the same choice as `shadow_mcp_disposition` and defaults to `allow_all`.
