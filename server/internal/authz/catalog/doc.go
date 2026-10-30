// Package catalog answers "may this caller do this to that resource?" for
// the permissions it declares, over the grants and engine package authz
// already has. It is option B of the RBAC evaluation, slice 1: configure an
// MCP server without using it.
//
// # How a decision is made
//
//  1. A service loads the resource and its containers from tenant-scoped
//     records and describes them as a Target (mcpserver.go: MCPServerTarget).
//  2. It asks for the operation's Requests through a builder in
//     mcpserver.go: MCPServerRead, MCPScopePin or MCPServerUse.
//  3. Client.Authorize (capabilities) or Client.Require (enforcement) checks
//     every request against its Definition before reading any grant: the
//     target must be the right kind, belong to the caller's organization,
//     and carry the containers the definition needs (client.go: prepare).
//  4. The Backend evaluates the remaining checks with authz.Engine and the
//     caller's prepared grants (backend.go).
//  5. The Decision says allowed, denied or unresolved, why, and whether the
//     allow rested on a legacy broad grant (client.go: decide).
//
// # Files
//
//   - permissions.go: the Definition of each permission: what implies it,
//     the block that withdraws it, whether a block always wins, where grants
//     may be anchored, and which containers a target must carry.
//   - client.go: Target, Request, Decision, and Client.
//   - backend.go: the Backend over authz.Engine.
//   - mcpserver.go: request builders for MCP servers.
//   - GUIDE.md: option B explained step by step, in plain words.
//   - walkthroughs/: one request followed end to end, with diagrams.
//     01-mcp-server-configure-vs-use.md: an editor pins a server's scopes
//     but is refused when using it.
//
// # Relationship to package authz
//
// authz cannot import this package, so the engine's own scope tables keep
// the MCP rules too: nothing implies mcp:connect (scopes.go) and an MCP
// block always wins (precedence.go: blockWinsScopes).
// TestDefinitionsMatchEngine fails if they ever disagree with permissions.go.
package catalog
