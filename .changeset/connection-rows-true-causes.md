---
"server": patch
---

Connection rows now name the client an MCP session reported before falling back to its User-Agent, and record a gateway member that is rate limited or returning errors. Tool results for those members now say the server is limiting requests or had an error. Several rows now name the real cause: a refused connection, a rejected shared credential or key, and a tunnel error versus an error from the server behind it.
