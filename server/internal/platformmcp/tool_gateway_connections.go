package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	listGatewayConnectionsToolName    = "list_gateway_connections"
	getGatewayConnectionTrendToolName = "get_gateway_connection_trend"
	gatewayConnectionsFeature         = "gateway_connections"
)

// registerGatewayConnectionsTool serves one manifest whether or not the
// session service is composed, so the tool never leaves the catalogue.
func registerGatewayConnectionsTool(reg *Registrar, service *GatewayConnectionsService) {
	handler := func(_ context.Context, _ *mcp.CallToolRequest, _ ListGatewayConnectionsInput) (*mcp.CallToolResult, ListGatewayConnectionsOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: gatewayConnectionsFeature, Message: "Gateway connections are unavailable on this server."})
		if err != nil {
			return nil, ListGatewayConnectionsOutput{}, fmt.Errorf("encode unavailable gateway connections: %w", err)
		}
		return nil, ListGatewayConnectionsOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
	if service.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input ListGatewayConnectionsInput) (*mcp.CallToolResult, ListGatewayConnectionsOutput, error) {
			return principalToolCall(ctx, gatewayConnectionsToolResult, func(principal Principal) (ListGatewayConnectionsOutput, error) {
				return service.List(ctx, principal, input)
			})
		}
	}

	// External only: the managed assistant already lists sessions through
	// its own platform tool, scoped to the assistant's project.
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:  listGatewayConnectionsToolName,
		Title: "List a Gateway's Connections",
		Description: "List the sessions people, agents, API keys and workloads hold on one gateway in an explicit project, as the gateway's Debug tab shows them: who is connected, from which client, to which upstream providers, and when each session was created, last used, expires or was revoked. " +
			"Use the gateway_id that create_gateway returns, or that get_plugin returns as target_id for a member whose backend is gateway. Narrow with search and the include and exclude filters; providers takes ids from upstream_provider_ids, or none for sessions with no admitted provider sign-in. Omitting status leaves out revoked sessions. Pages hold 50 sessions unless limit says otherwise, up to 100. " +
			"unattributed_count is how many sessions on the gateway's sign-in server match the same filters but recorded no gateway, so they cannot be listed here. " +
			"Read-only: it never revokes. Never returns tokens, token hashes or credentials. Agents are listed by identity without a name.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, handler)

	trendHandler := func(_ context.Context, _ *mcp.CallToolRequest, _ GatewayConnectionTrendInput) (*mcp.CallToolResult, GatewayConnectionTrendOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: gatewayConnectionsFeature, Message: "Gateway connections are unavailable on this server."})
		if err != nil {
			return nil, GatewayConnectionTrendOutput{}, fmt.Errorf("encode unavailable gateway connection trend: %w", err)
		}
		return nil, GatewayConnectionTrendOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
	if service.valid() {
		trendHandler = func(ctx context.Context, _ *mcp.CallToolRequest, input GatewayConnectionTrendInput) (*mcp.CallToolResult, GatewayConnectionTrendOutput, error) {
			return principalToolCall(ctx, gatewayConnectionsToolResult, func(principal Principal) (GatewayConnectionTrendOutput, error) {
				return service.Trend(ctx, principal, input)
			})
		}
	}
	// Both audiences: unlike the list, it returns bounded counts, read in the
	// exact project the caller names under its own grants.
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:  getGatewayConnectionTrendToolName,
		Title: "Count a Gateway's Connections Over Time",
		Description: "Count one gateway's connections over a window of at most 31 days (default the last 7) in an explicit project, as the gateway's Debug tab charts them: per hour or day bucket, the credentials first issued and those revoked before their deadline during it, and those active (unexpired, with no recorded revocation) immediately before its end. " +
			"A refresh continues a credential, so it is neither a creation nor a revocation; a credential that reaches its deadline is not counted as revoked. Active is a recorded lifetime: a refreshable credential before its authorization deadline or another before its token expires, with no revocation recorded; it is not request activity, effective permission, or proof that tool discovery works, and access-token revocations from before such revocations were recorded are not reflected. " +
			"active_at_start and active_at_end are the stock immediately before from and to, and active_at_end equals the last bucket's active. excluded_legacy_rows counts session records, not connections, left out because they were written before lineage was kept; unattributed_rows counts records on the gateway's current sign-in server that record no gateway. " +
			"Requires project:read on the project and mcp:read on the gateway; a gateway that is missing or not readable is not found. Never returns people, tokens or credentials.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, trendHandler)
}

func gatewayConnectionsToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Code: "", Feature: gatewayConnectionsFeature, Message: ""}
	shareable, isShareable := errors.AsType[*oops.ShareableError](err)
	switch {
	case errors.Is(err, errGatewayConnectionTrendInvalid):
		result.Code, result.Message = "invalid_request", "Provide an exact project_id and gateway_id, RFC 3339 from and to at most 31 days apart, and a bucket of hour or day."
	case errors.Is(err, errGatewayConnectionsInvalid):
		result.Code, result.Message = "invalid_request", fmt.Sprintf("Provide an exact project_id and gateway_id, a search of at most %d characters, and a limit from 1 to %d.", maxGatewayConnectionsSearchLength, maxGatewayConnectionsLimit)
	case isShareable && shareable.Code == oops.CodeBadRequest:
		result.Code, result.Message = "invalid_request", shareable.Error()
	case isShareable && shareable.Code == oops.CodeNotFound:
		result.Code, result.Message = "not_found", "That gateway is not in the selected project."
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{
		Meta: nil, Content: []mcp.Content{&mcp.TextContent{Text: string(content), Meta: nil, Annotations: nil}},
		StructuredContent: nil, IsError: true, InputRequests: nil, RequestState: "",
	}, true
}
