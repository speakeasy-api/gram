---
"server": patch
---

Served MCP requests are now recorded per connection, at most every five minutes and never more than five minutes behind the latest one, so failing connections can be told apart from working ones. A tool call or resource read that returned an error does not count as served. Nothing reads these records yet. When a tool call needs a server's tool list and the server never answers, this is now recorded as the server being unreachable. Request failure rows now name the person's own connection to the service when their sign-in was used.
