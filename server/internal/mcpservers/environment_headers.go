package mcpservers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// EnvironmentHeaderInspector reads the MCP_HEADER_ entries of an environment
// for a names-only preview.
type EnvironmentHeaderInspector interface {
	InspectMCPHeaders(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) (environments.MCPHeaderEnvironment, error)
	MCPHeaderNearMissNames(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) ([]string, error)
}

// WithEnvironmentHeaders sets the reader behind GetEnvironmentHeaders.
func (s *Service) WithEnvironmentHeaders(inspector EnvironmentHeaderInspector) *Service {
	if s != nil {
		s.environmentHeaders = inspector
	}
	return s
}

const (
	environmentSelectionLinked      = "linked"
	environmentSelectionEnvironment = "environment"
	environmentSelectionNone        = "none"

	environmentStatusNone        = "none"
	environmentStatusOK          = "ok"
	environmentStatusUnavailable = "unavailable"

	environmentHeaderOverridesSource = "overrides_source"
	environmentHeaderNotMapped       = "not_mapped"
)

// GetEnvironmentHeaders previews, by name only, the upstream headers a proxied
// MCP server sends from its linked environment or from a candidate one. It
// shares the serving path's inspection, so a status here is the outcome
// serving would reach, and it never contacts the upstream.
func (s *Service) GetEnvironmentHeaders(ctx context.Context, payload *gen.GetEnvironmentHeadersPayload) (*gen.McpServerEnvironmentHeaders, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, s.logger)
	}
	candidate, err := parseEnvironmentSelection(payload)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogWarn(ctx, s.logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, s.logger)
	}

	// Authorize before reading anything about any environment: the same
	// project-wide authority linking requires, plus read access to the server.
	if err := s.authz.Require(ctx,
		authz.MCPCheck(authz.ScopeMCPRead, grantResourceID(server.ID, server.ToolsetID), projectID.String()),
		authz.EnvironmentLinkCheck(projectID.String()),
	); err != nil {
		return nil, err
	}

	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "environment headers apply only to remote and tunneled MCP servers").LogWarn(ctx, s.logger)
	}
	if s.environmentHeaders == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "environment header inspector is not configured").LogError(ctx, s.logger)
	}

	options, err := environmentsrepo.New(s.db).ListEnvironments(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list environments").LogError(ctx, s.logger)
	}
	result := &gen.McpServerEnvironmentHeaders{
		Environment:                     nil,
		EnvironmentStatus:               environmentStatusNone,
		EnvironmentConfigurationInvalid: false,
		Entries:                         []*gen.McpServerEnvironmentHeader{},
		Environments:                    make([]*gen.McpServerEnvironmentSummary, 0, len(options)),
	}
	for _, env := range options {
		result.Environments = append(result.Environments, &gen.McpServerEnvironmentSummary{ID: env.ID.String(), Name: env.Name, Slug: env.Slug})
	}

	environmentID := server.EnvironmentID
	switch payload.Selection {
	case environmentSelectionNone:
		environmentID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	case environmentSelectionEnvironment:
		environmentID = uuid.NullUUID{UUID: candidate, Valid: true}
	}
	if !environmentID.Valid {
		return result, nil
	}

	env, err := s.environmentHeaders.InspectMCPHeaders(ctx, projectID, environmentID.UUID)
	switch {
	case errors.Is(err, environments.ErrEnvironmentUnavailable):
		result.EnvironmentStatus = environmentStatusUnavailable
		result.EnvironmentConfigurationInvalid = true
		return result, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "inspect environment headers").LogError(ctx, s.logger)
	}

	sourceNames, err := s.sourceHeaderNames(ctx, server)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list source header names").LogError(ctx, s.logger)
	}
	nearMiss, err := s.environmentHeaders.MCPHeaderNearMissNames(ctx, projectID, env.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list near-miss environment entries").LogError(ctx, s.logger)
	}

	result.Environment = &gen.McpServerEnvironmentSummary{ID: env.ID.String(), Name: env.Name, Slug: env.Slug}
	result.EnvironmentStatus = environmentStatusOK
	for _, header := range env.Headers {
		status := string(header.Status)
		if header.Status == proxy.EnvironmentHeaderMapped {
			if overridesSource(header.HeaderName, sourceNames) {
				status = environmentHeaderOverridesSource
			}
		} else {
			result.EnvironmentConfigurationInvalid = true
		}
		var headerName *string
		if header.HeaderName != "" {
			headerName = new(header.HeaderName)
		}
		result.Entries = append(result.Entries, &gen.McpServerEnvironmentHeader{EntryName: header.EntryName, HeaderName: headerName, Status: status})
	}
	for _, name := range nearMiss {
		result.Entries = append(result.Entries, &gen.McpServerEnvironmentHeader{EntryName: name, HeaderName: nil, Status: environmentHeaderNotMapped})
	}
	return result, nil
}

// parseEnvironmentSelection validates the selection and returns the candidate
// environment for selection "environment".
func parseEnvironmentSelection(payload *gen.GetEnvironmentHeadersPayload) (uuid.UUID, error) {
	hasCandidate := payload.EnvironmentID != nil && *payload.EnvironmentID != ""
	switch payload.Selection {
	case environmentSelectionEnvironment:
		if !hasCandidate {
			return uuid.Nil, errors.New("environment_id is required when selection is environment")
		}
		candidate, err := uuid.Parse(*payload.EnvironmentID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid environment_id: %w", err)
		}
		return candidate, nil
	case environmentSelectionLinked, environmentSelectionNone:
		if hasCandidate {
			return uuid.Nil, fmt.Errorf("environment_id is only accepted when selection is environment")
		}
		return uuid.Nil, nil
	default:
		return uuid.Nil, fmt.Errorf("invalid selection %q", payload.Selection)
	}
}

// sourceHeaderNames lists the names, never the values, of the headers the
// server's source configures.
func (s *Service) sourceHeaderNames(ctx context.Context, server repo.McpServer) ([]string, error) {
	switch {
	case server.RemoteMcpServerID.Valid:
		rows, err := remotemcprepo.New(s.db).ListServerHeaders(ctx, remotemcprepo.ListServerHeadersParams{
			RemoteMcpServerID: server.RemoteMcpServerID.UUID,
			ProjectID:         server.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("list remote mcp server headers: %w", err)
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, row.Name)
		}
		return names, nil
	case server.TunneledMcpServerID.Valid:
		rows, err := tunneledmcprepo.New(s.db).ListServerHeaders(ctx, tunneledmcprepo.ListServerHeadersParams{
			TunneledMcpServerID: server.TunneledMcpServerID.UUID,
			ProjectID:           server.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("list tunneled mcp server headers: %w", err)
		}
		names := make([]string, 0, len(rows))
		for _, row := range rows {
			names = append(names, row.Name)
		}
		return names, nil
	default:
		return nil, nil
	}
}

func overridesSource(headerName string, sourceNames []string) bool {
	for _, name := range sourceNames {
		if proxy.SameHeaderField(headerName, name) {
			return true
		}
	}
	return false
}
