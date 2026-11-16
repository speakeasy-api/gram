---
"dashboard": minor
"server": minor
---

A Shadow MCP server can be linked to an official version that Speakeasy serves, from its own page. When the project already serves the same URL, one click links that server. Otherwise the page checks that Speakeasy can reach the server, that it is an MCP server, and how sign-in can be set up, then Make official opens a sheet with everything filled in. Identity and session issuer choices sit under Details. The new server is linked as it is made, and opens in a new tab to inspect and try. When a server cannot be made official automatically, the page says what is in the way, links to its catalog entry when the catalog has the same URL, and otherwise takes a request for help.

While a link is on, direct connections are blocked wherever a Shadow MCP policy applies. The block message names the official server, or the gateway that serves it, and its address, so the agent can switch. The link can be switched off while the official server is tested, switched on again, or removed so another server can be linked. Under Advanced, any server in the project can be linked. Each change is recorded in the audit log.

The URL check reports how Speakeasy can register with a server's sign-in provider: dynamic client registration, a client ID metadata document, or neither.
