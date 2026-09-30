//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type MCPNetworkTrafficReader interface {
	GetMCPNetworkTraffic(context.Context, telemetryrepo.GetMCPNetworkTrafficParams) ([]telemetryrepo.MCPNetworkTrafficRow, error)
}

type MCPNetworkTrafficInput struct {
	ProjectID  string `json:"project_id" jsonschema:"exact project ID"`
	TargetKind string `json:"target_kind" jsonschema:"mcp or gateway"`
	TargetID   string `json:"target_id" jsonschema:"exact MCP server or gateway ID"`
	Window     string `json:"window,omitempty" jsonschema:"24h or 7d (default)"`
}

type MCPNetworkRouteSummary struct {
	Requests   uint64 `json:"requests"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

type MCPNetworkTrafficOutput struct {
	ProjectID  string                 `json:"project_id"`
	TargetKind string                 `json:"target_kind"`
	TargetID   string                 `json:"target_id"`
	Window     string                 `json:"window"`
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Public     MCPNetworkRouteSummary `json:"public"`
	Private    MCPNetworkRouteSummary `json:"private"`
	Caveat     string                 `json:"caveat"`
}

func (r *PostgresReader) WithMCPNetworkTraffic(reader MCPNetworkTrafficReader, logsEnabled FeatureChecker) *PostgresReader {
	if r != nil && reader != nil && logsEnabled != nil {
		r.networkTraffic = reader
		r.networkTrafficLogsEnabled = logsEnabled
	}
	return r
}

func (r *PostgresReader) GetMCPNetworkTraffic(ctx context.Context, principal Principal, input MCPNetworkTrafficInput) (MCPNetworkTrafficOutput, error) {
	if r == nil || r.db == nil || r.authz == nil || r.networkTraffic == nil || r.networkTrafficLogsEnabled == nil {
		return MCPNetworkTrafficOutput{}, ErrUnavailable
	}
	project, err := r.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID})
	if err != nil {
		return MCPNetworkTrafficOutput{}, err
	}
	targetID, err := uuid.Parse(input.TargetID)
	if err != nil || (input.TargetKind != "mcp" && input.TargetKind != "gateway") {
		return MCPNetworkTrafficOutput{}, fmt.Errorf("target_kind must be mcp or gateway and target_id must be a UUID")
	}
	window := input.Window
	if window == "" {
		window = "7d"
	}
	hours := 168
	if window == "24h" {
		hours = 24
	} else if window != "7d" {
		return MCPNetworkTrafficOutput{}, fmt.Errorf("window must be 24h or 7d")
	}

	kind := telemetryrepo.MCPNetworkTrafficServerKindMCP
	resourceID := targetID.String()
	if input.TargetKind == "mcp" {
		server, err := mcpserversrepo.New(r.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: targetID, ProjectID: project.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return MCPNetworkTrafficOutput{}, ErrMCPConnectionSettingsNotFound
		}
		if err != nil {
			return MCPNetworkTrafficOutput{}, fmt.Errorf("load MCP server: %w", err)
		}
		if server.ToolsetID.Valid {
			resourceID = server.ToolsetID.UUID.String()
		}
	} else {
		kind = telemetryrepo.MCPNetworkTrafficServerKindMeta
		_, err := metamcprepo.New(r.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: targetID, ProjectID: project.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return MCPNetworkTrafficOutput{}, ErrMCPConnectionSettingsNotFound
		}
		if err != nil {
			return MCPNetworkTrafficOutput{}, fmt.Errorf("load gateway: %w", err)
		}
	}
	if err := r.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, resourceID, project.ID.String())); err != nil {
		if isAuthorizationDenied(err) {
			return MCPNetworkTrafficOutput{}, ErrMCPConnectionSettingsNotFound
		}
		return MCPNetworkTrafficOutput{}, err
	}
	enabled, err := r.networkTrafficLogsEnabled(ctx, principal.OrganizationID)
	if err != nil {
		return MCPNetworkTrafficOutput{}, fmt.Errorf("check observability: %w", err)
	}
	if !enabled {
		return MCPNetworkTrafficOutput{}, &ToolRefusalError{Code: "observability_disabled", Payload: "Enable observability for this organization to inspect network traffic."}
	}
	to := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	from := to.Add(-time.Duration(hours) * time.Hour)
	rows, err := r.networkTraffic.GetMCPNetworkTraffic(ctx, telemetryrepo.GetMCPNetworkTrafficParams{GramProjectID: project.ID.String(), ServerKind: kind, ServerID: targetID.String(), From: from, To: to})
	if err != nil {
		return MCPNetworkTrafficOutput{}, fmt.Errorf("read MCP network traffic: %w", err)
	}
	output := MCPNetworkTrafficOutput{ProjectID: project.ID.String(), TargetKind: input.TargetKind, TargetID: input.TargetID, Window: window, From: from.Format(time.RFC3339), To: to.Format(time.RFC3339), Caveat: "Only requests observed while telemetry was enabled are counted. No observed requests does not prove clients have migrated."}
	var publicLast, privateLast time.Time
	for _, row := range rows {
		switch row.Surface {
		case "public":
			output.Public.Requests += row.RequestCount
			if row.LastSeen.After(publicLast) {
				publicLast = row.LastSeen
			}
		case "private":
			output.Private.Requests += row.RequestCount
			if row.LastSeen.After(privateLast) {
				privateLast = row.LastSeen
			}
		}
	}
	if !publicLast.IsZero() {
		output.Public.LastSeenAt = publicLast.UTC().Format(time.RFC3339)
	}
	if !privateLast.IsZero() {
		output.Private.LastSeenAt = privateLast.UTC().Format(time.RFC3339)
	}
	return output, nil
}

const mcpNetworkTrafficToolName = "get_mcp_network_traffic"

func registerMCPNetworkTrafficTool(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name: mcpNetworkTrafficToolName, Title: "Check MCP Network Traffic",
		Description: "Summarize observed public and private inbound requests and last-seen times for one exact MCP server or gateway over 24 hours or 7 days. Use before changing its network access mode; a quiet route is not proof all clients migrated.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input MCPNetworkTrafficInput) (*mcp.CallToolResult, MCPNetworkTrafficOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCPNetworkTrafficOutput{}, err
		}
		output, err := reader.GetMCPNetworkTraffic(ctx, principal, input)
		return nil, output, err
	})
}

func registerUnavailableMCPNetworkTrafficTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name: mcpNetworkTrafficToolName, Title: "Check MCP Network Traffic",
		Description: "Summarize observed public and private inbound requests for one MCP server or gateway. Traffic reporting is not available yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPRead}, func(_ context.Context, _ *mcp.CallToolRequest, _ MCPNetworkTrafficInput) (*mcp.CallToolResult, MCPNetworkTrafficOutput, error) {
		return nil, MCPNetworkTrafficOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: "Traffic reporting is not available yet."}
	})
}
