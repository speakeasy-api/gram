package admin

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetExternalMCPMigrator enables the external MCP migration methods.
func (s *Service) SetExternalMCPMigrator(migrator *migration.Migrator) {
	s.externalMCPMigrator = migrator
}

func (s *Service) ListExternalMcpMigrationCandidates(ctx context.Context, _ *gen.ListExternalMcpMigrationCandidatesPayload) (*gen.AdminListExternalMcpMigrationCandidatesResult, error) {
	if s.externalMCPMigrator == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	candidates, err := s.externalMCPMigrator.ListCandidates(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list external MCP migration candidates").LogError(ctx, s.logger)
	}

	views := make([]*gen.AdminExternalMcpMigrationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		row := candidate.Row
		var toolCalls *int64
		if candidate.ToolCalls != nil {
			count, _ := conv.ClampedUint64ToInt64(*candidate.ToolCalls)
			toolCalls = &count
		}
		blockers := candidate.Blockers
		if blockers == nil {
			blockers = []string{}
		}
		views = append(views, &gen.AdminExternalMcpMigrationCandidate{
			ToolsetID:        row.ToolsetID.String(),
			OrganizationID:   row.OrganizationID,
			OrganizationName: row.OrganizationName,
			OrganizationSlug: row.OrganizationSlug,
			ProjectID:        row.ProjectID.String(),
			ProjectName:      row.ProjectName,
			ProjectSlug:      row.ProjectSlug,
			ServerName:       row.ToolsetName,
			McpSlug:          conv.PtrEmpty(candidate.Row.EndpointSlug),
			RemoteURL:        conv.PtrEmpty(candidate.RemoteURL),
			ReplacementURL:   conv.PtrEmpty(candidate.ReplacementURL),
			SignInProvider:   conv.PtrEmpty(candidate.SignInProvider()),
			ToolCalls:        toolCalls,
			Action:           string(candidate.Action),
			DeleteReasons:    nonNil(candidate.DeleteReasons),
			Blockers:         blockers,
			Warnings:         nonNil(candidate.Warnings),
		})
	}
	return &gen.AdminListExternalMcpMigrationCandidatesResult{
		Candidates:         views,
		ToolCallWindowDays: int(migration.ToolCallWindow.Hours() / 24),
	}, nil
}

func (s *Service) PreviewExternalMcpMigration(ctx context.Context, payload *gen.PreviewExternalMcpMigrationPayload) (*gen.AdminExternalMcpMigrationResult, error) {
	return s.runExternalMCPMigration(ctx, payload.Servers, true)
}

func (s *Service) MigrateExternalMcpServers(ctx context.Context, payload *gen.MigrateExternalMcpServersPayload) (*gen.AdminExternalMcpMigrationResult, error) {
	return s.runExternalMCPMigration(ctx, payload.Servers, false)
}

func (s *Service) runExternalMCPMigration(ctx context.Context, servers []*gen.AdminExternalMcpMigrationRequest, dryRun bool) (*gen.AdminExternalMcpMigrationResult, error) {
	if s.externalMCPMigrator == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	// A migration writes into customer projects, so it is only ever audited
	// under the staff member who ran it.
	authCtx, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == "" || authCtx.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	requests := make([]migration.Request, 0, len(servers))
	seen := make(map[uuid.UUID]bool, len(servers))
	for _, server := range servers {
		toolsetID, err := uuid.Parse(server.ToolsetID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset id").LogWarn(ctx, s.logger)
		}
		if seen[toolsetID] {
			return nil, oops.E(oops.CodeBadRequest, nil, "a server is listed more than once").LogWarn(ctx, s.logger)
		}
		seen[toolsetID] = true
		requests = append(requests, migration.Request{ToolsetID: toolsetID})
	}

	actor := migration.Actor{UserID: authCtx.OIDCSubject, Email: conv.PtrEmpty(authCtx.Email)}
	outcomes := s.externalMCPMigrator.Run(ctx, actor, requests, dryRun)

	views := make([]*gen.AdminExternalMcpMigrationOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		views = append(views, &gen.AdminExternalMcpMigrationOutcome{
			ToolsetID:         outcome.ToolsetID.String(),
			Action:            conv.PtrEmpty(string(outcome.Action)),
			MigrationStatus:   string(outcome.Status),
			Reason:            conv.PtrEmpty(outcome.Reason),
			McpSlug:           conv.PtrEmpty(outcome.McpSlug),
			McpServerID:       uuidOrNil(outcome.McpServerID),
			RemoteMcpServerID: uuidOrNil(outcome.RemoteMcpServerID),
			RemoteURL:         conv.PtrEmpty(outcome.RemoteURL),
			Actions:           nonNil(outcome.Actions),
			Warnings:          nonNil(outcome.Warnings),
		})
	}
	return &gen.AdminExternalMcpMigrationResult{Outcomes: views}, nil
}

func uuidOrNil(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	return new(id.String())
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
