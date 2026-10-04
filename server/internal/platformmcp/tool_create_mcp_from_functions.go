//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	createMCPFromFunctionsToolName           = "create_mcp_from_functions"
	unavailableCreateMCPFromFunctionsMessage = "Creating an MCP server from a project's functions is not available on this server."
)

// registerCreateMCPFromFunctionsTool registers the live and unavailable paths
// with one manifest, so the tool never appears on and disappears from the
// catalogue as a deployment composes or fails to compose the service.
func registerCreateMCPFromFunctionsTool(reg *Registrar, service *MCPToolExposureService) {
	handler := unavailableToolExposureHandler[CreateMCPFromFunctionsInput, CreateMCPFromFunctionsOutput](unavailableCreateMCPFromFunctionsMessage)
	if service.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input CreateMCPFromFunctionsInput) (*mcp.CallToolResult, CreateMCPFromFunctionsOutput, error) {
			return principalToolCall(ctx, toolExposureToolResult, func(principal Principal) (CreateMCPFromFunctionsOutput, error) {
				return service.CreateMCPFromFunctions(ctx, principal, input)
			})
		}
	}

	// External only, like the other writes that decide what a server exposes:
	// it is an administrator's authoring decision, made on the surface they
	// drive directly.
	addTool(reg, &mcp.Tool{
		Name:  createMCPFromFunctionsToolName,
		Title: "Create an MCP Server from Functions",
		Description: "Create a new MCP server in an explicit project whose tools are tools the project's functions already produce. Use this when no existing server fits; to put a tool on a server that already exists, use add_tools_to_mcp instead. " +
			"Supply a server name, exact function tool URNs from list_project_tools, and one idempotency key for this creation. Call it first without confirmed: true: that creates and records nothing and returns a confirmation_required refusal whose preview holds the exact toolset_slug and the mcp_slug_prefix the server's address starts with; a short suffix is added to that prefix at creation and the result reports the full mcp_slug. Show those to the user, and only after they confirm the exact project, name, preview, and tool list call again with the same request, the same idempotency key, and confirmed: true. Use a new key only for a new attempt after a refusal. " +
			"A tool that the project's latest completed deployment does not produce is refused by name and nothing is created; a tool pushed after that deployment finished is not available until the new one completes. Tools generated from an API document are refused: this creates servers from functions only. " +
			"Usually the new server reaches nobody until it is put into a plugin with distribute_mcp_to_plugin. The exception is an organization's first server, which joins the project's Default plugin on creation, as it does from the dashboard, so everyone holding that plugin receives it; the preview's would_join_default_plugin says whether that applies, so say so before confirming, and report added_to_default_plugin and publication_requested from the result rather than assuming either. publication_requested means a refresh of people's plugins was requested, not confirmed delivered, so never say people already have the server. A retry with the same idempotency key returns the server already created instead of creating another.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly,
		ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPWrite,
	}, handler)
}
