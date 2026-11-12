package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// assistantIdentityMigrationTimeout bounds one organization's migration. A run
// cut short commits nothing and is finished by running again.
const assistantIdentityMigrationTimeout = 5 * time.Minute

// ErrAssistantMigrationOrganizationNotFound means no organization has the ID.
var ErrAssistantMigrationOrganizationNotFound = errors.New("no organization with that ID")

const (
	assistantIdentityMigrated        = "migrated"
	assistantIdentityAlreadyMigrated = "already_migrated"
	assistantIdentityNeedsOwner      = "needs_owner"
	assistantIdentityFailed          = "failed"
)

// SetAssistantIdentities enables the assistant identity migration. Without
// one, as on an admin server with no Speakeasy signing issuer, it reports
// unavailable.
func (s *Service) SetAssistantIdentities(identities *assistantidentity.Service) {
	s.assistantIdentities = identities
}

// ListOrganizationAssistants lists one organization's live assistants with
// their identity state.
func (s *Service) ListOrganizationAssistants(ctx context.Context, p *gen.ListOrganizationAssistantsPayload) (*gen.AdminOrganizationAssistants, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	assistants, err := organizationAssistants(ctx, s.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization assistants").LogError(ctx, logger)
	}
	return &gen.AdminOrganizationAssistants{OrganizationID: organizationID, Assistants: assistants}, nil
}

// MigrateOrganizationAssistantIdentities gives every assistant of one
// organization without a dedicated agent its own agent, attributed to the
// staff member running it.
func (s *Service) MigrateOrganizationAssistantIdentities(ctx context.Context, p *gen.MigrateOrganizationAssistantIdentitiesPayload) (*gen.AdminAssistantIdentityMigrationRun, error) {
	if s.assistantIdentities == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "assistant identity migration is not available on this server").LogWarn(ctx, s.logger)
	}
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	actor, displayName, _ := adminActor(ctx)
	staff := &assistantidentity.StaffActor{Principal: actor, DisplayName: displayName}

	// The run outlives the request, so a closed tab does not abandon it.
	detached := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithTimeout(detached, assistantIdentityMigrationTimeout)
	defer cancel()

	tx, err := s.db.Begin(runCtx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin assistant identity migration").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(detached) })
	run, err := MigrateAssistantIdentitiesTx(runCtx, tx, logger, s.assistantIdentities, organizationID, staff)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate assistant identities").LogError(ctx, logger)
	}
	if err := tx.Commit(runCtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit assistant identity migration").LogError(ctx, logger)
	}

	logger.InfoContext(detached, "assistant identity migration ran",
		attr.SlogAuditAction("assistant_identity_migration:run"),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	return run, nil
}

// MigrateAssistantIdentitiesTx gives every assistant of organizationID without
// a dedicated agent its own agent, attributed to staff, inside tx. Each
// assistant runs under its own savepoint, so one that fails is reported and
// rolled back without the others. The admin app and Admin MCP share it.
func MigrateAssistantIdentitiesTx(ctx context.Context, tx pgx.Tx, logger *slog.Logger, identities *assistantidentity.Service, organizationID string, staff *assistantidentity.StaffActor) (*gen.AdminAssistantIdentityMigrationRun, error) {
	if _, err := repo.New(tx).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false}); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssistantMigrationOrganizationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read assistant migration organization: %w", err)
	}
	assistants, err := organizationAssistants(ctx, tx, organizationID)
	if err != nil {
		return nil, err
	}
	run := &gen.AdminAssistantIdentityMigrationRun{OrganizationID: organizationID, Migrated: 0, AlreadyMigrated: 0, NeedsOwner: 0, Failed: 0, Results: make([]*gen.AdminAssistantIdentityMigrationResult, 0, len(assistants))}
	for _, assistant := range assistants {
		outcome := assistantIdentityAlreadyMigrated
		if assistant.IdentityState == string(assistantidentity.NeverConfigured) {
			outcome = provisionAssistantIdentity(ctx, tx, logger, identities, organizationID, assistant, staff)
		}
		switch outcome {
		case assistantIdentityMigrated:
			run.Migrated++
		case assistantIdentityAlreadyMigrated:
			run.AlreadyMigrated++
		case assistantIdentityNeedsOwner:
			run.NeedsOwner++
		default:
			run.Failed++
		}
		run.Results = append(run.Results, &gen.AdminAssistantIdentityMigrationResult{AssistantID: assistant.ID, ProjectID: assistant.ProjectID, Name: assistant.Name, Outcome: outcome})
	}
	return run, nil
}

func provisionAssistantIdentity(ctx context.Context, tx pgx.Tx, logger *slog.Logger, identities *assistantidentity.Service, organizationID string, assistant *gen.AdminOrganizationAssistant, staff *assistantidentity.StaffActor) string {
	logger = logger.With(attr.SlogProjectID(assistant.ProjectID), attr.SlogAssistantID(assistant.ID))
	err := func() error {
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin assistant identity savepoint: %w", err)
		}
		defer o11y.NoLogDefer(func() error { return savepoint.Rollback(ctx) })
		if err := identities.Provision(ctx, savepoint, assistantidentity.ProvisionParams{
			OrganizationID: organizationID, ProjectID: uuid.MustParse(assistant.ProjectID), AssistantID: uuid.MustParse(assistant.ID),
			ActorUserID: "", Staff: staff,
		}); err != nil {
			return fmt.Errorf("provision assistant identity: %w", err)
		}
		if err := savepoint.Commit(ctx); err != nil {
			return fmt.Errorf("release assistant identity savepoint: %w", err)
		}
		return nil
	}()
	switch {
	case err == nil:
		return assistantIdentityMigrated
	case errors.Is(err, assistantidentity.ErrOwnerUnavailable):
		return assistantIdentityNeedsOwner
	default:
		logger.ErrorContext(ctx, "migrate assistant identity", attr.SlogError(err))
		return assistantIdentityFailed
	}
}

func organizationAssistants(ctx context.Context, db repo.DBTX, organizationID string) ([]*gen.AdminOrganizationAssistant, error) {
	rows, err := repo.New(db).AdminListOrganizationAssistants(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("read organization assistants: %w", err)
	}
	byProject := make(map[uuid.UUID][]uuid.UUID)
	for _, row := range rows {
		byProject[row.ProjectID] = append(byProject[row.ProjectID], row.ID)
	}
	states := make(map[uuid.UUID]assistantidentity.AssistantState, len(rows))
	for project, ids := range byProject {
		projectStates, err := assistantidentity.States(ctx, db, project, ids)
		if err != nil {
			return nil, fmt.Errorf("read assistant identity states: %w", err)
		}
		maps.Copy(states, projectStates)
	}
	assistants := make([]*gen.AdminOrganizationAssistant, 0, len(rows))
	for _, row := range rows {
		state := states[row.ID]
		var agentID *string
		if state.AgentID != nil {
			agentID = new(state.AgentID.String())
		}
		assistants = append(assistants, &gen.AdminOrganizationAssistant{
			ID: row.ID.String(), Name: row.Name,
			ProjectID: row.ProjectID.String(), ProjectSlug: row.ProjectSlug, ProjectName: row.ProjectName,
			Status: row.Status, IdentityState: string(state.State), AgentID: agentID, RuntimeVersion: int(row.RuntimeVersion),
			CreatedByUserID: conv.FromPGText[string](row.CreatedByUserID), CreatedByUserEmail: conv.FromPGText[string](row.CreatedByUserEmail),
			CreatorActive: row.CreatorActive, CreatedAt: row.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	return assistants, nil
}
