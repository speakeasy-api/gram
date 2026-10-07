---
"server": minor
"dashboard": minor
---

Add `remoteMcp.getServerScopes` and `remoteMcp.setServerScopePin` so a remote-backed MCP server's protected resource scope pin can be read and set, including servers whose resource was never discovered. The pin is shared by every server in the project with the same upstream URL, so both endpoints require write access to all of them. The read reports what a login through each bound client would request now and whether the pin applies to it.

Add a "Pinned scopes" picker under the connected identity provider on a Remote MCP server's Identity settings, showing whether the pin is used for that connection; it is read-only for organizations without resource scope discovery unless a pin needs clearing. Saving only header or pin edits no longer re-commits the connected client.
