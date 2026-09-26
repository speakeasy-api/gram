package telemetry

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcpRepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

const (
	mcpNetworkTrafficSurfacePublic  = "public"
	mcpNetworkTrafficSurfacePrivate = "private"
)

// mcpNetworkTrafficWindowHours maps the API window to a count of hourly
// buckets. The current, partial hour is always the final bucket.
var mcpNetworkTrafficWindowHours = map[string]int{
	"24h": 24,
	"7d":  7 * 24,
}

// GetMcpNetworkTraffic returns hourly observed public and private request
// counts for one MCP server or gateway in the caller's project. Counts only
// cover requests Gram observed while telemetry logs were enabled, so a quiet
// surface is evidence, not proof, that clients have moved.
func (s *Service) GetMcpNetworkTraffic(ctx context.Context, payload *telem_gen.GetMcpNetworkTrafficPayload) (*telem_gen.GetMcpNetworkTrafficResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	hours, ok := mcpNetworkTrafficWindowHours[payload.Window]
	if !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "window must be one of 24h or 7d")
	}
	serverKind, serverID, resourceID, err := s.resolveMCPNetworkTrafficServer(ctx, *authCtx.ProjectID, payload)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, resourceID, authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, telemetryerrs.ErrLogsDisabled, "logs are not enabled for this organization")
	}

	to := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	from := to.Add(-time.Duration(hours) * time.Hour)

	rows, err := s.chRepo.GetMCPNetworkTraffic(ctx, repo.GetMCPNetworkTrafficParams{
		GramProjectID: authCtx.ProjectID.String(),
		ServerKind:    serverKind,
		ServerID:      serverID.String(),
		From:          from,
		To:            to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching mcp network traffic")
	}

	return buildMCPNetworkTrafficResult(from, to, hours, rows), nil
}

// resolveMCPNetworkTrafficServer validates that exactly one server ID was
// supplied and that it belongs to the caller's project.
func (s *Service) resolveMCPNetworkTrafficServer(ctx context.Context, projectID uuid.UUID, payload *telem_gen.GetMcpNetworkTrafficPayload) (string, uuid.UUID, string, error) {
	hasMCP := payload.McpServerID != nil && *payload.McpServerID != ""
	hasMeta := payload.MetaMcpServerID != nil && *payload.MetaMcpServerID != ""
	if hasMCP == hasMeta {
		return "", uuid.Nil, "", oops.E(oops.CodeBadRequest, nil, "exactly one of mcp_server_id or meta_mcp_server_id is required")
	}

	if hasMCP {
		id, err := uuid.Parse(*payload.McpServerID)
		if err != nil {
			return "", uuid.Nil, "", oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id")
		}
		server, err := mcpserversRepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversRepo.GetMCPServerByIDAndProjectIDParams{ID: id, ProjectID: projectID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return "", uuid.Nil, "", oops.E(oops.CodeNotFound, err, "mcp server not found")
		case err != nil:
			return "", uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "error loading mcp server")
		}
		resourceID := server.ID.String()
		if server.ToolsetID.Valid {
			resourceID = server.ToolsetID.UUID.String()
		}
		return repo.MCPNetworkTrafficServerKindMCP, id, resourceID, nil
	}

	id, err := uuid.Parse(*payload.MetaMcpServerID)
	if err != nil {
		return "", uuid.Nil, "", oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id")
	}
	_, err = metamcpRepo.New(s.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcpRepo.GetMetaMCPServerByIDAndProjectIDParams{ID: id, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", uuid.Nil, "", oops.E(oops.CodeNotFound, err, "gateway not found")
	case err != nil:
		return "", uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "error loading gateway")
	}
	return repo.MCPNetworkTrafficServerKindMeta, id, id.String(), nil
}

// buildMCPNetworkTrafficResult zero-fills every hour in [from, to) so the
// chart shows quiet hours explicitly rather than interpolating across them.
func buildMCPNetworkTrafficResult(from, to time.Time, hours int, rows []repo.MCPNetworkTrafficRow) *telem_gen.GetMcpNetworkTrafficResult {
	points := make([]*telem_gen.McpNetworkTrafficPoint, hours)
	for i := range points {
		points[i] = &telem_gen.McpNetworkTrafficPoint{
			BucketStart:     from.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
			PublicRequests:  0,
			PrivateRequests: 0,
		}
	}

	var lastPublic, lastPrivate time.Time
	for _, row := range rows {
		idx := int(row.Hour.UTC().Sub(from) / time.Hour)
		if idx < 0 || idx >= hours {
			continue
		}
		switch row.Surface {
		case mcpNetworkTrafficSurfacePublic:
			points[idx].PublicRequests += uint64ToInt64(row.RequestCount)
			if row.LastSeen.After(lastPublic) {
				lastPublic = row.LastSeen
			}
		case mcpNetworkTrafficSurfacePrivate:
			points[idx].PrivateRequests += uint64ToInt64(row.RequestCount)
			if row.LastSeen.After(lastPrivate) {
				lastPrivate = row.LastSeen
			}
		}
	}

	return &telem_gen.GetMcpNetworkTrafficResult{
		From:          from.Format(time.RFC3339),
		To:            to.Format(time.RFC3339),
		Points:        points,
		LastPublicAt:  formatOptionalTime(lastPublic),
		LastPrivateAt: formatOptionalTime(lastPrivate),
	}
}

func formatOptionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}
