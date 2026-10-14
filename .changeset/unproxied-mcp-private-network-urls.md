---
"server": minor
---

Unproxied MCP servers can now be added with URLs that only resolve or route inside the client's network, such as tailnet or VPN hosts. Clients connect to these servers directly, so Speakeasy no longer looks up or connects to their URL: creating one checks only that the URL is a well-formed http or https address, no default icon is fetched from the vendor's site, and the unused `unproxiedMcp.listTools` endpoint, which connected to the server to list its tools, is removed.
