//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const getMCPConnectionSettingsToolName = "get_mcp_connection_settings"

func registerMCPConnectionSettingsTool(reg *Registrar, service *MCPConnectionSettingsService) {
	addTool(reg, &mcp.Tool{
		Name:        getMCPConnectionSettingsToolName,
		Title:       "Get MCP Connection Settings",
		Description: "Read one exact MCP server or gateway in an explicit project: stored network mode and visibility, endpoint IDs/slugs/domains/root markers, configured ingress state, and direct plugin memberships. Ingress DNS/status are observations, not proof of client connectivity; endpoints are not necessarily publisher-selected package URLs. No concurrency-safe mutation version is available. Never returns upstream URLs, credentials, tokens, or other secrets.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPConnectionSettingsInput) (*mcp.CallToolResult, MCPConnectionSettings, error) {
		return principalToolCall(ctx, mcpConnectionSettingsToolResult, func(principal Principal) (MCPConnectionSettings, error) {
			return service.Get(ctx, principal, input)
		})
	})
}

func registerUnavailableMCPConnectionSettingsTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        getMCPConnectionSettingsToolName,
		Title:       "Get MCP Connection Settings",
		Description: "Read the current connection settings for one exact MCP server or gateway in an explicit project. Connection settings are unavailable on this server.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(_ context.Context, _ *mcp.CallToolRequest, _ GetMCPConnectionSettingsInput) (*mcp.CallToolResult, featureUnavailableResult, error) {
		return nil, featureUnavailableResult{Code: unavailableCode, Feature: "mcp_connection_settings", Message: "MCP connection settings are unavailable on this server."}, nil
	})
}

func mcpConnectionSettingsToolResult(err error) (*mcp.CallToolResult, bool) {
	var result featureUnavailableResult
	switch {
	case errors.Is(err, ErrMCPConnectionSettingsInvalid):
		result = featureUnavailableResult{Code: "invalid_request", Feature: "mcp_connection_settings", Message: "Provide a project ID, an exact target ID, and target_kind set to mcp_server or gateway."}
	case errors.Is(err, ErrMCPConnectionSettingsNotFound):
		result = featureUnavailableResult{Code: "not_found", Feature: "mcp_connection_settings", Message: "That target is not available in the selected project."}
	case errors.Is(err, ErrUnavailable):
		result = featureUnavailableResult{Code: unavailableCode, Feature: "mcp_connection_settings", Message: "MCP connection settings are temporarily unavailable."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
