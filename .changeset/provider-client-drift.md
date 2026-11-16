---
"server": minor
"dashboard": minor
---

An MCP Authentication Provider's page warns when the remote session clients it binds don't match the ones its MCP servers use, and shows which clients differ on each side.

The new `listRemoteSessionClientBindings` endpoint is marked deprecated from the start: the remote session clients bound to a provider are no longer authoritative, and readers are moving to each MCP server's own client associations. The endpoint exists only to surface drift during that migration.
