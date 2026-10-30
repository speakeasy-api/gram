# Add an MCP server from the catalog and connect to it

A user finds a third-party MCP server in the catalog, adds it to their project,
and gets the URL an agent connects to.

## Given

- The catalog is reachable (it is fetched live from the MCP registry).
- "DeepWiki" is in the catalog. Its remote needs no credentials, so the add
  flow needs no headers or identity setup.
- The user can write MCP servers.

## When

1. Open the MCP catalog.
2. Search for "DeepWiki".
3. Add it to the project, named `Flow catalog <runId>`. Skip guardrails if
   the add flow offers them.
4. From the confirmation, open the new server.

## Then

- The add flow confirms the server was added and shows its MCP endpoint URL,
  of the form `<server URL>/mcp/<slug>`.
- The user lands on the new server's page.
- `mcpServers.list` includes a server named `Flow catalog <runId>`.
- The MCP endpoint URL answers an MCP `initialize` request (the user can
  connect).
