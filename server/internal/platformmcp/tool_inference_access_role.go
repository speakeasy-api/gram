//nolint:exhaustruct // MCP manifests rely on SDK defaults.
package platformmcp

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerInferenceAccessRoleTool(reg *Registrar, mutations *AccessRoleMutationService) {
	handler := unavailableAccessRoleMutationHandler[ConfigureInferenceAccessRoleInput, ConfigureInferenceAccessRoleOutput]()
	if mutations != nil && mutations.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input ConfigureInferenceAccessRoleInput) (*mcp.CallToolResult, ConfigureInferenceAccessRoleOutput, error) {
			return principalToolCall(ctx, accessRoleMutationToolResult, func(principal Principal) (ConfigureInferenceAccessRoleOutput, error) {
				return mutations.ConfigureInference(ctx, principal, input)
			})
		}
	}
	addTool(reg, &mcp.Tool{
		Name: operationConfigureInferenceAccessRole, Title: "Configure Inference Access Role",
		Description: "Add or remove only the selected inference:execute grant for one explicit project, or every project when all_projects is true, on a selected custom role, preserving all other grants and assignments. First read list_access_roles for its reference and version; confirm the grant scope, role and impact on all role holders and affected projects. project_id remains required for admission and receipt scoping, including all-project writes. Requires organization administration, explicit confirmation, expected version and a stable idempotency key. Returns a historical committed receipt and a fresh exact-grant check, not effective workload access. Removing this grant does not revoke other project-specific or broader grants. Both agent and owner need explicit inference permission. Configure direct agent inference grants in the dashboard agent access controls, or assign suitable roles through dashboard access controls; this tool only edits custom role grants. Existing MCP-only assignment tools cannot assign this inference role. Does not exchange workload tokens, expose credentials, or configure managed assistants. If verification fails after commit, retry identical inputs and key.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, handler)
}
