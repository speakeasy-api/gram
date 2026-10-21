package admin

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) ListUpstreamOAuthMigrationOrganizations(ctx context.Context, _ *gen.ListUpstreamOAuthMigrationOrganizationsPayload) (*gen.AdminUpstreamOAuthMigrationOrganizations, error) {
	rows, err := repo.New(s.db).AdminListUpstreamOAuthMigrationOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list upstream oauth migration organizations").LogError(ctx, s.logger)
	}
	organizations := make([]*gen.AdminUpstreamOAuthMigrationOrganization, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, &gen.AdminUpstreamOAuthMigrationOrganization{
			ID:      row.ID,
			Name:    row.Name,
			Slug:    row.Slug,
			Servers: row.Servers,
		})
	}
	return &gen.AdminUpstreamOAuthMigrationOrganizations{Organizations: organizations}, nil
}

func (s *Service) GetUpstreamOAuthMigrationStatus(ctx context.Context, p *gen.GetUpstreamOAuthMigrationStatusPayload) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.upstreamOAuthMigrationStatus(ctx, organizationID)
}

func (s *Service) RunUpstreamOAuthMigration(ctx context.Context, p *gen.RunUpstreamOAuthMigrationPayload) (*gen.AdminUpstreamOAuthMigrationRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	// Associations first: a credential's choice is only recorded for a client
	// associated with the server.
	associations, err := associationbackfill.Run(ctx, s.db, associationbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate upstream oauth client associations").LogError(ctx, logger)
	}
	selections, err := selectionbackfill.Run(ctx, s.db, selectionbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate upstream oauth client selections").LogError(ctx, logger)
	}
	// Typed auth clients last, from the associations just recorded. A failure
	// here is reported beside the run and never fails the migration above.
	var authClientsError *string
	authClients, err := authclientbackfill.Run(ctx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	if err != nil {
		logger.ErrorContext(ctx, "record typed auth clients", attr.SlogError(err))
		message := "Recording typed auth clients failed. Run again; the upstream OAuth migration is unaffected."
		authClientsError = &message
	}
	status, err := s.upstreamOAuthMigrationStatus(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	logger.InfoContext(ctx, "upstream oauth migration ran",
		attr.SlogAuditAction("upstream_oauth_migration:run"),
		attr.SlogAuditSubject("organization"),
		attr.SlogAuditSubjectID(organizationID),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return &gen.AdminUpstreamOAuthMigrationRun{
		AssociationsWritten:                int64(associations.Written),
		AssociationsSkipped:                int64(associations.Skipped),
		SelectionsWritten:                  selections.Written,
		AuthClientsWritten:                 new(int64(authClients.ClientsWritten)),
		AuthClientLinksWritten:             new(int64(authClients.LinksWritten)),
		ServersMarkedRequiringUpstreamAuth: new(int64(authClients.ServersMarked)),
		AuthClientsError:                   authClientsError,
		Status:                             status,
	}, nil
}

// upstreamOAuthMigrationStatus dry-runs both backfills for one organization.
func (s *Service) upstreamOAuthMigrationStatus(ctx context.Context, organizationID string) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	associations, err := associationbackfill.Run(ctx, s.db, associationbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check upstream oauth client associations").LogError(ctx, logger)
	}
	selections, err := selectionbackfill.Run(ctx, s.db, selectionbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check upstream oauth client selections").LogError(ctx, logger)
	}

	unresolved := make([]*gen.AdminUpstreamOAuthMigrationServer, 0, associations.Gaps)
	listed := map[uuid.UUID]bool{}
	addUnresolved := func(serverID, projectID uuid.UUID) {
		if !listed[serverID] {
			listed[serverID] = true
			unresolved = append(unresolved, &gen.AdminUpstreamOAuthMigrationServer{McpServerID: serverID.String(), ProjectID: projectID.String()})
		}
	}
	for _, result := range associations.Results {
		if !result.Outcome.Covered() {
			addUnresolved(result.MCPServerID, result.ProjectID)
		}
	}
	for _, result := range associations.GatewayResults {
		if !result.Outcome.Covered() {
			addUnresolved(result.MCPServerID, result.ProjectID)
		}
	}
	reconnect := map[uuid.UUID]bool{}
	for _, gap := range selections.Gaps {
		if gap.Outcome == selectionbackfill.OutcomeAmbiguous || gap.Outcome == selectionbackfill.OutcomeUnreachableClient || gap.Outcome == selectionbackfill.OutcomeUnusableClient {
			reconnect[gap.CredentialID] = true
		}
	}
	pendingSelections := int64(selections.Pairs[selectionbackfill.OutcomeClient] + selections.Pairs[selectionbackfill.OutcomeNoAuth])

	return &gen.AdminUpstreamOAuthMigrationStatus{
		AuthClients:          s.authClientMigrationStatus(ctx, organizationID),
		OrganizationID:       organizationID,
		Migrated:             associations.Decided == 0 && len(unresolved) == 0 && pendingSelections == 0,
		Servers:              int64(associations.Servers),
		PendingAssociations:  int64(associations.Decided),
		UnresolvedServers:    unresolved,
		Credentials:          int64(selections.Credentials),
		PendingSelections:    pendingSelections,
		ReconnectCredentials: int64(len(reconnect)),
	}, nil
}

// authClientMigrationStatus dry-runs the typed auth client backfill. It is
// reported apart from migrated and never gates the cutover, so a failure only
// leaves it out.
func (s *Service) authClientMigrationStatus(ctx context.Context, organizationID string) *gen.AdminUpstreamOAuthMigrationAuthClients {
	report, err := authclientbackfill.Run(ctx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		s.logger.With(attr.SlogOrganizationID(organizationID)).ErrorContext(ctx, "check typed auth clients", attr.SlogError(err))
		return nil
	}
	unresolved := []*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{}
	for _, result := range report.Results {
		if result.Outcome != authclientbackfill.OutcomeAmbiguous {
			continue
		}
		var authClientID *string
		if result.AuthClientID.Valid {
			authClientID = new(result.AuthClientID.UUID.String())
		}
		unresolved = append(unresolved, &gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{
			AssociationID:         result.AssociationID.String(),
			McpServerID:           result.MCPServerID.String(),
			ProjectID:             result.ProjectID.String(),
			RemoteSessionClientID: result.RemoteSessionClientID.String(),
			AuthClientID:          authClientID,
		})
	}
	pendingRetirements := int64(report.PendingRetiredClients + report.PendingRetiredLinks)
	return &gen.AdminUpstreamOAuthMigrationAuthClients{
		Complete:               report.PendingClients == 0 && report.PendingLinks == 0 && report.Gaps == 0 && pendingRetirements == 0 && report.PendingMarkedServers == 0,
		Associations:           int64(report.Associations),
		PendingClients:         int64(report.PendingClients),
		PendingLinks:           int64(report.PendingLinks),
		PendingRetirements:     pendingRetirements,
		PendingMarkedServers:   int64(report.PendingMarkedServers),
		Unresolved:             int64(len(unresolved)),
		UnresolvedAssociations: unresolved,
	}
}
