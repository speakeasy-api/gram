package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MCPNetworkRequestEventURN identifies the telemetry row written once per
// resolved inbound MCP HTTP request. The mcp_network_traffic_hourly_summaries
// materialized view filters on it.
var MCPNetworkRequestEventURN = urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, "mcp_network_request").String()

// recordMCPNetworkRequest writes one observation of an inbound MCP request and
// the network surface it arrived on, feeding the public versus private
// traffic graph. Exactly one of mcpServerID or metaServerID is set.
//
// The row deliberately carries no tool URN or trace ID so it never counts as
// a tool call in trace or metrics summaries. Best effort and off the request
// path: failures are logged by the telemetry logger and never fail the request.
func (s *Service) recordMCPNetworkRequest(ctx context.Context, projectID, mcpServerID, metaServerID uuid.UUID, organizationID string) {
	if projectID == uuid.Nil || (mcpServerID == uuid.Nil && metaServerID == uuid.Nil) {
		return
	}
	if organizationID == "" {
		organizationID = ingressOrganizationID(ctx)
	}

	attrs := map[attr.Key]any{
		attr.EventURNKey:       MCPNetworkRequestEventURN,
		attr.NetworkSurfaceKey: string(mcpmetrics.NetworkSurfaceFromContext(ctx)),
	}
	if mcpServerID != uuid.Nil {
		attrs[attr.McpServerIDKey] = mcpServerID.String()
	} else {
		attrs[attr.MetaMcpServerIDKey] = metaServerID.String()
	}
	observedAt := time.Now()

	go s.writeMCPNetworkRequest(context.WithoutCancel(ctx), projectID, organizationID, observedAt, attrs)
}

func (s *Service) writeMCPNetworkRequest(ctx context.Context, projectID uuid.UUID, organizationID string, observedAt time.Time, attrs map[attr.Key]any) {
	if organizationID == "" {
		project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
		if err != nil {
			s.logger.WarnContext(ctx, "resolve organization for mcp network traffic", attr.SlogError(err), attr.SlogProjectID(projectID.String()))
			return
		}
		organizationID = project.OrganizationID
	}

	s.telemLogger.Log(ctx, tm.LogParams{
		Timestamp: observedAt,
		ToolInfo: tm.ToolInfo{
			ID:             "",
			URN:            "",
			Name:           "",
			ProjectID:      projectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: organizationID,
		},
		UserInfo:   tm.UserInfoByID(""),
		Attributes: attrs,
	})
}

// ingressOrganizationID returns the organization the ingress already
// established for this request, or "" when only the project row knows it.
func ingressOrganizationID(ctx context.Context) string {
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.OrganizationID != "" {
		return origin.OrganizationID
	}
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil && domainCtx.OrganizationID != "" {
		return domainCtx.OrganizationID
	}
	return ""
}
