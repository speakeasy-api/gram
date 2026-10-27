---
"server": minor
"dashboard": minor
---

Remote session clients on an organization's authorization server can be duplicated from the Clients list or the client's detail page. The copy keeps the original's settings and, by default, its client ID and secret, or registers a new client with the authorization server when it supports dynamic registration. A client metadata document client gets its own document. The copy opens for editing right away, and MCP servers keep using the original.
