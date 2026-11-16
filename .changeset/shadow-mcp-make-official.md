---
"dashboard": minor
"server": minor
---

A Shadow MCP server can be made official from its page. The page checks that Speakeasy can reach the server, that it is an MCP server, and that sign-in can be set up, then opens the new-server form with the URL and name filled in. When a server cannot be made official automatically, the page says what is in the way and takes a request for help. A server the project already serves reads as Onboarded, with the address agents connect to, and the list puts the most used servers first.

Direct connections to an onboarded server are blocked wherever a Shadow MCP policy applies. The block message names the official server, or the gateway that serves it, and its address, so the agent can switch.

Under Advanced, closed by default, an admin can choose any server in the project as a Shadow MCP server's official version, and can keep direct connections working while the official server is tested. Such a server shows as Testing until direct connections are blocked again. Each change is recorded in the audit log.

The URL check reports how Speakeasy can register with a server's sign-in provider: dynamic client registration, a client ID metadata document, or neither. A provider that supports neither needs a client registered by hand. When the Speakeasy catalog has an entry for the same URL, the Shadow MCP page links to it, since the catalog adds the server and its settings take the hand-registered client. Otherwise the page offers a request for help instead of Make official.
