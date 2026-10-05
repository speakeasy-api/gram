//nolint:exhaustruct // MCP manifests and service payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type xaaConnectionsReader interface {
	List(context.Context, *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error)
}

type xaaReadinessService struct {
	connections xaaConnectionsReader
	enabled     FeatureChecker
}

// WithXAAReadiness reuses the dashboard service's org-admin authorization,
// MCP visibility filtering and readiness derivation. List itself is not gated,
// so the agent surface checks the same rollout flag before reading it.
func (r *PostgresReader) WithXAAReadiness(connections xaaConnectionsReader, flags feature.Provider) *PostgresReader {
	r.xaaReadiness = &xaaReadinessService{connections: connections, enabled: func(ctx context.Context, orgID string) (bool, error) {
		if flags == nil {
			return false, ErrUnavailable
		}
		slug, err := NewPostgresOrganizationSlugResolver(r.db).OrganizationSlug(ctx, orgID)
		if err != nil {
			return false, err
		}
		return flags.IsFlagEnabled(ctx, feature.FlagOktaConnections, orgID, feature.OrgProjectGroups(slug, ""))
	}}
	return r
}

type GetXAAReadinessInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project ID that owns the MCP server"`
	MCPServerID string `json:"mcp_server_id" jsonschema:"exact MCP server ID, not a Platform MCP registration or legacy toolset ID"`
}

// Deliberately omit organization totals, other servers, provider app IDs,
// client IDs, scopes, URLs and administrator-entered labels from List.
type GetXAAReadinessOutput struct {
	ProjectID           string  `json:"project_id"`
	MCPServerID         string  `json:"mcp_server_id"`
	State               string  `json:"state"`
	Pending             bool    `json:"pending"`
	NotApplicableReason *string `json:"not_applicable_reason,omitempty"`
	BrokenReason        *string `json:"broken_reason,omitempty"`
	ObservedResult      *string `json:"observed_result,omitempty"`
	ObservedAt          *string `json:"observed_at,omitempty"`
}

func registerXAAReadinessTool(reg *Registrar, service *xaaReadinessService) {
	addTool(reg, &mcp.Tool{
		Name: "get_xaa_readiness", Title: "Check Cross-App Access Readiness",
		Description: "Inspect stored Okta Cross-App Access (XAA) readiness for one exact project and MCP server. Reports the dashboard's derived state and exchange evidence; does not probe, connect or change anything. Connected means administrator-confirmed, not verified. Requires organization administration and read access to the server. Unlike get_mcp_readiness, this checks organization XAA configuration, not a registration's provider readiness.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: discoveryMCPRead, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetXAAReadinessInput) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
		// External-only: the service reads an org-wide snapshot under a live member
		// user context. Project assistants have no reviewed org-admin authority for
		// that service. Never expose that snapshot to a managed assistant.
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetXAAReadinessOutput{}, err
		}
		if principal.surface() == SurfaceProjectAssistant {
			return xaaReadinessRefusal("forbidden", "XAA readiness requires an external organization administrator.")
		}
		projectID, err := uuid.Parse(input.ProjectID)
		if err != nil || projectID == uuid.Nil {
			return xaaReadinessRefusal("invalid_request", "Provide an exact project ID and MCP server ID.")
		}
		serverID, err := uuid.Parse(input.MCPServerID)
		if err != nil || serverID == uuid.Nil {
			return xaaReadinessRefusal("invalid_request", "Provide an exact project ID and MCP server ID.")
		}
		if service == nil || service.connections == nil || service.enabled == nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is unavailable on this server.")
		}
		enabled, err := service.enabled(ctx, principal.OrganizationID)
		if err != nil {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		if !enabled {
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is not enabled for this organization.")
		}
		result, err := service.connections.List(ctx, &srv.ListPayload{IncludeAll: true})
		if err != nil {
			if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
				if shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized {
					return xaaReadinessRefusal("forbidden", "XAA readiness requires organization administration and server read access.")
				}
				if shareable.Code == oops.CodeFailedPrecondition {
					return xaaReadinessRefusal("setup_required", "Connect an identity provider before checking XAA readiness.")
				}
			}
			return xaaReadinessRefusal(unavailableCode, "XAA readiness is temporarily unavailable.")
		}
		if result != nil {
			for _, row := range result.Servers {
				if row != nil && row.ProjectID == projectID.String() && row.McpServerID == serverID.String() {
					return nil, GetXAAReadinessOutput{ProjectID: row.ProjectID, MCPServerID: row.McpServerID, State: row.State, Pending: row.Pending, NotApplicableReason: row.NotApplicableReason, BrokenReason: row.BrokenReason, ObservedResult: row.ObservedResult, ObservedAt: row.ObservedAt}, nil
				}
			}
		}
		return xaaReadinessRefusal("not_found", "That project or eligible MCP server is not available to you.")
	})
}

func xaaReadinessRefusal(code, message string) (*mcp.CallToolResult, GetXAAReadinessOutput, error) {
	payload, err := json.Marshal(featureUnavailableResult{Code: code, Feature: "xaa_readiness", Message: message})
	if err != nil {
		return nil, GetXAAReadinessOutput{}, fmt.Errorf("marshal XAA readiness refusal: %w", err)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}}, GetXAAReadinessOutput{}, nil
}
