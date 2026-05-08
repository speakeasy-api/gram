---
"server": minor
---

Add `remoteMcp.verifyURL` for probing a candidate remote MCP server URL by issuing an MCP `initialize` request and reporting whether the URL is reachable. A `401` or `403` response counts as verified — auth verification is intentionally out of scope.
