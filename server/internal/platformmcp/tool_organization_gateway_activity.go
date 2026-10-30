//nolint:exhaustruct // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const operationGetOrganizationGatewayActivity = "get_organization_gateway_activity"

const organizationGatewayActivityDescription = "Summarize calls dispatched through this organization's Organization Gateway across every project over a recent window: dispatched and failed calls, the discovery steps agents took (list_servers, describe_server, describe_tools), and the busiest MCP servers with their errors. Each server carries its mcp_id and project_id for use with get_mcp and the diagnostics tools. Constraints: aggregated server-side; carries the window it covers, how fresh the observations are, and whether there were any. No users, clients, or tokens are returned."

// Organization-scoped like the dashboard tab, which requires org:read.
var organizationGatewayActivityMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: discoveryOrgRead}

func registerOrganizationGatewayActivityTool(reg *Registrar, service *OrganizationGatewayActivityService) {
	if !service.valid() {
		registerUnavailableOrganizationGatewayActivityTool(reg)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        operationGetOrganizationGatewayActivity,
		Title:       "Organization Gateway Activity",
		Description: organizationGatewayActivityDescription,
		Annotations: readOnlyAnnotations(),
	}, organizationGatewayActivityMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetOrganizationGatewayActivityInput) (*mcp.CallToolResult, GetOrganizationGatewayActivityOutput, error) {
		return principalToolCall(ctx, organizationGatewayActivityToolResult, func(principal Principal) (GetOrganizationGatewayActivityOutput, error) {
			return service.Get(ctx, principal, input)
		})
	})
}

// The unavailable tool keeps the live input type, so the advertised schema
// does not change with the deployment.
func registerUnavailableOrganizationGatewayActivityTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        operationGetOrganizationGatewayActivity,
		Title:       "Organization Gateway Activity",
		Description: "Summarize calls dispatched through the Organization Gateway across every project. This is not available on this deployment.",
		Annotations: readOnlyAnnotations(),
	}, organizationGatewayActivityMeta, func(_ context.Context, _ *mcp.CallToolRequest, _ GetOrganizationGatewayActivityInput) (*mcp.CallToolResult, featureUnavailableResult, error) {
		result := featureUnavailableResult{Code: unavailableCode, Feature: organizationGatewayActivityFeature, Message: "Organization Gateway activity is not available on this deployment."}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, featureUnavailableResult{}, fmt.Errorf("encode unavailable organization gateway activity: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, result, nil
	})
}

func organizationGatewayActivityToolResult(err error) (*mcp.CallToolResult, bool) {
	if !errors.Is(err, ErrOrganizationGatewayActivityUnavailable) {
		return operationBudgetToolResult(err)
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: organizationGatewayActivityFeature, Message: "Organization Gateway activity is temporarily unavailable."}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
