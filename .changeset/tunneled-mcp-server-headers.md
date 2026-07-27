---
"server": minor
---

Add configurable request header support for tunneled MCP servers. Headers can be
a static value (optionally secret, encrypted at rest) or a pass-through of a
named inbound request header, and are injected onto the request forwarded
through the tunnel to the customer's upstream MCP server.
