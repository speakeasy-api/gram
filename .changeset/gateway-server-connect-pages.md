---
"server": minor
---

Authorizing an MCP gateway no longer asks you to connect every service before you can approve it. The consent page shows how many of the gateway's servers are connected, a list of those servers, and a Give access button that works before any service is connected. The list opens by default when the gateway has five servers or fewer; longer lists start collapsed behind Show servers. Each server shows its status. A server that signs in through one service has its own Connect or Reconnect button in the list, which goes straight to the provider and returns to the list.

Each server also has its own connect page, which lists only that server's services and has no separate approval, since connecting is all the server needs. It is where you manage a connected server, and where you connect a server that signs in through more than one service. When a gateway tool call reaches a server you haven't connected, or whose connection was rejected, the error includes a link to that server's connect page, which signs you in and goes straight to the provider. This applies to organization gateways and to gateways built in a project.
