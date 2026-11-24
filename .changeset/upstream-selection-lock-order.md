---
"server": patch
---

Signing in to an MCP server no longer fails now and then with a database deadlock while a recently used server in the same project is having its protected resource metadata refreshed. Recording which upstream client a new session uses now locks the project's servers in the same order the refresh does.
