---
"server": patch
---

Served MCP requests are now recorded once per connection, at most every five minutes, so failing connections can be told apart from working ones. Nothing reads these records yet.
