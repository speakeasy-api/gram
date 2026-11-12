//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var accessPermissionsMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeNone, DiscoveryScopes: discoveryOrgRead}

// registerAccessPermissionsTool serves the scope catalogue. It needs no
// service: the catalogue is product configuration, the same for every
// organization, so both access read registrations (live and unavailable)
// register it live.
func registerAccessPermissionsTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "describe_access_permissions",
		Title:       "Describe Access Permissions",
		Description: "Describe every permission a role can hold, in the words the dashboard uses: its label, what a holder can do, how it is grouped, which permissions it includes, and whether it is sensitive and why. Also lists tasks, the jobs that need several permissions together, so a missing one can be explained in context. This is product configuration, the same for every organization: it names no role, member, grant or resource. Use it to explain what a permission or role gives, and name any sensitive permission a role to be created or assigned carries before asking for confirmation.",
		Annotations: readOnlyAnnotations(),
	}, accessPermissionsMeta, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, DescribeAccessPermissionsOutput, error) {
		return nil, describeAccessPermissions(), nil
	})
}
