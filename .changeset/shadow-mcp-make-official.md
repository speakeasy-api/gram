---
"dashboard": minor
"server": minor
---

A Shadow MCP server can be linked to an official version that Speakeasy serves, from its own page. The page recommends the quickest way and keeps every other way under Advanced: link the project server that already serves the same URL, add the server's Speakeasy catalog entry, make a new server when Speakeasy can register itself with the sign-in provider, or set one up by hand. Adding from the catalog and making a new server both happen in a sheet on the same page, with everything filled in and identity choices under Details. When the provider needs a hand-registered client, the sheet takes it with the server's own Identity settings, then turns the server on. The new server is linked as it is made, and opens in a new tab to inspect and try. When nothing can be set up automatically, the page takes a request for help.

While a link is on, direct connections are blocked wherever a Shadow MCP policy applies. The block message names the official server, or the gateway that serves it, and its address, so the agent can switch. The link can be switched off while the official server is tested, so direct connections follow the policy's usual rules and the server shows its ordinary access status, switched on again, or removed so another server can be linked. Under Advanced, any server in the project can be linked. Each change is recorded in the audit log.

The URL check reports how Speakeasy can register with a server's sign-in provider: dynamic client registration, a client ID metadata document, or neither.
