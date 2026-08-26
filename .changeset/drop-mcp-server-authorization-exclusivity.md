---
"server": patch
---

Allow an MCP server to record both its Gram-issued user session issuer and the authorization server its upstream authenticates against, and index MCP servers by user session issuer so recomputing that record does not scan the table.
