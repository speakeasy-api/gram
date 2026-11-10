---
"server": patch
---

With upstream auth enforcement on, an MCP server offers only the upstream sign-in clients configured for it. A server no longer picks up a client another server on the same User Identity issuer uses, or one a deleted server left behind. A server created on or moved onto an issuer it alone uses still offers the clients bound there. Deleting a server, gateway or toolset now also removes the sign-in bindings that gateway membership created for it.
