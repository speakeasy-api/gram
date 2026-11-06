# Build an MCP server from an existing source

A user turns an OpenAPI document their project already has into a hosted MCP
server, then changes which tools it exposes.

## Given

- The project has a completed deployment with the "Acme Internal API" OpenAPI
  source and its 8 HTTP tools (seeded).
- The user can write MCP servers.

## When

1. From the MCP servers list, start adding a new server.
2. Choose to build it from an existing source.
3. Pick the "Acme Internal API" source and name the server `Flow source <runId>`.
4. Create the server.
5. On the new server's tools, remove the `check_health` tool.
6. Add the `check_health` tool back.

## Then

- The user lands on the new server's tools page.
- After step 4, `toolsets.get` for the new server lists all 8
  `tools:http:acme:*` tool URNs.
- After step 5, it lists 7 and not `tools:http:acme:check_health`.
- After step 6, it lists all 8 again.
