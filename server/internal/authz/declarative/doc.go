// Package declarative decides MCP server permissions from a declared
// relationship schema over the stored principal_grants rows (RBAC option D).
//
// A request flows through four pieces, each in its own file:
//
//  1. mcp_server_schema.go is the whole policy, written with builders: the
//     types (organization → project → mcp_server), the relations a stored
//     scope grants on them, and the permissions computed from those
//     relations. read and edit have no edge to connect, so managing a server
//     never grants use of it, and every permission excludes its block.
//  2. relschema/ is the machinery, with no MCP knowledge. builders.go
//     assembles declarations, schema.go validates them (Compile), decode.go
//     turns each stored row into a relationship or an error, and eval.go
//     answers one permission on one object with a trace.
//  3. mcp_server_access.go is what services call. RequireMCPServer enforces
//     a permission and MCPServerCapabilities reports all three; both use the
//     one decision function, decideMCPServer, which reads the caller's
//     admitted policies from the authz engine (Engine.PolicySet) and requires
//     every policy to allow. ExplainMCPServer exposes the same decision with
//     its trace for inspection, not enforcement.
//  4. Activation is per organization (authz EngineOpts.MCPServerSchemaActive).
//     An organization that is not active keeps the legacy authz check
//     unchanged.
//
// Services identify the operation and pass a trusted MCPServerRef built from
// a tenant-scoped row; they make no policy decisions. The call sites are
// mcpservers.UpdateMcpServer and GetMcpServer (edit, read and the reported
// permissions) and the private endpoint check in mcp/serveendpoint.go (use).
package declarative
