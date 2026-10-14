---
"server": minor
---

Authorizing an MCP gateway no longer asks you to connect every service up front. The consent page shows how many of the gateway's servers are connected and a Give access button, which works before any service is connected. Show servers lists each server you can use with its status, and each one opens its own connect page, where you sign in to the services that server uses. Connecting there is all a server needs, so the page has no separate approval.

When a gateway tool call reaches a server you haven't connected, or whose connection was rejected, the error now includes a link to that server's connect page. Opening it signs you in and takes you straight to the server's provider. This applies to organization gateways and to gateways built in a project alike.
