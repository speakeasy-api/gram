---
"dashboard": patch
"server": patch
---

An MCP-scoped risk policy no longer requires at least one tool per selected server. An empty tool list (or unchecking every tool in the dashboard scope picker) is now normalized to "every tool on this server," matching what happens when no tool selection is made at all, instead of being rejected or silently dropping the server from the policy's scope.
