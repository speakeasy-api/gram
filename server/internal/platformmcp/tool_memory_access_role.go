//nolint:exhaustruct // MCP manifests rely on SDK defaults.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerMemoryAccessRoleTool(reg *Registrar, mutations *AccessRoleMutationService) {
	handler := unavailableAccessRoleMutationHandler[ConfigureMemoryAccessRoleInput, ConfigureMemoryAccessRoleOutput]()
	if mutations != nil && mutations.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input ConfigureMemoryAccessRoleInput) (*mcp.CallToolResult, ConfigureMemoryAccessRoleOutput, error) {
			return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (ConfigureMemoryAccessRoleOutput, error) {
				return mutations.ConfigureMemory(ctx, principal, input)
			})
		}
	}
	addTool(reg, &mcp.Tool{
		Name: operationConfigureMemoryAccessRole, Title: "Configure Team Memory Access Role",
		Description: "Add or remove only the selected team memory grant, memory:read or memory:write, for one explicit project on a selected custom role, preserving all other grants and assignments. memory:read lets role holders search the project's masked session summaries, memos and digests; memory:write also lets them save memos and includes memory:read. Neither grants raw transcripts. First read list_access_roles for the role's reference and version; confirm the project, scope, role and that every holder of the role gains or loses access. Requires organization administration, explicit confirmation, expected version and a stable idempotency key. Returns a historical committed receipt and a fresh exact-grant check, not effective access: removing this grant does not revoke a broader grant elsewhere on the role. Assign the role to people through dashboard access controls; the MCP-only assignment tool cannot assign it. If verification fails after commit, retry identical inputs and key.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, handler)
}
