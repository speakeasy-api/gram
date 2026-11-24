package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/networkingress/ingresscredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// tailscaleOAuthMinimumDualWrite is how long an organization's Tailscale OAuth
// credentials stay in dual_write before finalize is allowed. The reconciler
// reopens an ingress's credentials only after its last health check is an
// hour stale, on an hourly sweep with jitter, so a day is sure to cover real
// reads through the organization key while the platform-key copies still exist.
const tailscaleOAuthMinimumDualWrite = orgencryption.DefaultMinimumDualWrite

// tailscaleOAuthStepTimeout bounds one migration step, which outlives the
// request so a closed tab does not cut a backfill or clear short. Each step
// touches a handful of rows; the clear waits a settle delay between passes.
const tailscaleOAuthStepTimeout = 10 * time.Minute

// TailscaleOAuthMigration is what the temporary Tailscale OAuth client
// migration needs from the process's organization encryption.
type TailscaleOAuthMigration struct {
	// Transitions reads and changes the organizations' transition state.
	Transitions *orgencryption.Transitions

	// Runner backfills and verifies the organization-key copies.
	Runner *orgencryption.Runner

	// Store reaches the network ingress credential copies.
	Store orgencryption.CopyStore
}

// SetTailscaleOAuthMigration enables the temporary Tailscale OAuth client
// migration endpoints. Without it they report unavailable.
func (s *Service) SetTailscaleOAuthMigration(migration *TailscaleOAuthMigration) {
	s.tailscaleOAuthMigration = migration
}

func (s *Service) ListTailscaleOAuthMigration(ctx context.Context, _ *gen.ListTailscaleOAuthMigrationPayload) (*gen.AdminTailscaleOAuthMigrationOrganizations, error) {
	migration := s.tailscaleOAuthMigration
	if migration == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "organization encryption is not configured in this admin process").LogWarn(ctx, s.logger)
	}

	rows, err := repo.New(s.db).AdminListTailscaleOAuthMigrationOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations with Tailscale OAuth credentials").LogError(ctx, s.logger)
	}

	organizations := make([]*gen.AdminTailscaleOAuthMigrationOrganization, 0, len(rows))
	for _, row := range rows {
		organization, err := migration.status(ctx, row.ID, row.Name, row.Slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read Tailscale OAuth migration status").LogError(ctx, s.logger.With(attr.SlogOrganizationID(row.ID)))
		}
		organizations = append(organizations, organization)
	}

	return &gen.AdminTailscaleOAuthMigrationOrganizations{
		Organizations:           organizations,
		MinimumDualWriteSeconds: int64(max(tailscaleOAuthMinimumDualWrite, orgencryption.MinimumDualWriteFloor) / time.Second),
	}, nil
}

func (s *Service) RunTailscaleOAuthMigrationStep(ctx context.Context, payload *gen.RunTailscaleOAuthMigrationStepPayload) (*gen.AdminTailscaleOAuthMigrationOrganization, error) {
	migration := s.tailscaleOAuthMigration
	if migration == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "organization encryption is not configured in this admin process").LogWarn(ctx, s.logger)
	}

	principal, displayName, _ := adminActor(ctx)
	actor := orgencryption.Actor{Principal: principal, DisplayName: displayName}

	organization, err := repo.New(s.db).AdminGetTailscaleOAuthMigrationOrganization(ctx, payload.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "organization not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve organization").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogOrganizationID(organization.ID), attr.SlogOrgEncryptionUseCase(ingresscredentials.UseCase))

	// The step outlives the request, so a closed tab does not cut a backfill
	// or clear short; every step is safe to run again.
	stepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tailscaleOAuthStepTimeout)
	defer cancel()

	stores := []orgencryption.CopyStore{migration.Store}
	switch payload.Step {
	case "enroll":
		_, err = migration.Transitions.Enroll(stepCtx, organization.ID, ingresscredentials.UseCase, actor)
	case "backfill":
		_, err = migration.Runner.Run(stepCtx, organization.ID, ingresscredentials.UseCase, stores)
	case "finalize":
		_, err = migration.Transitions.Finalize(stepCtx, organization.ID, ingresscredentials.UseCase, orgencryption.FinalizeRequest{MinimumDualWrite: tailscaleOAuthMinimumDualWrite, Stores: stores}, actor)
	case "clear_platform_copies":
		_, err = migration.Transitions.ClearPlatformCopies(stepCtx, organization.ID, ingresscredentials.UseCase, stores, actor)
	case "rollback":
		_, err = migration.Transitions.Rollback(stepCtx, organization.ID, ingresscredentials.UseCase, actor)
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown migration step %q", payload.Step)
	}
	switch {
	case errors.Is(err, orgencryption.ErrTransitionNotAllowed), errors.Is(err, orgencryption.ErrFinalizeBlocked),
		errors.Is(err, orgencryption.ErrFenced), errors.Is(err, orgencryption.ErrConcurrentChange):
		return nil, oops.E(oops.CodeConflict, err, "%s", err.Error()).LogInfo(ctx, logger)
	case err != nil && orgencryption.IsKeyProblem(err):
		return nil, oops.E(oops.CodeUnavailable, err, "the organization's encryption key is unavailable: %s", err.Error()).LogWarn(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "run Tailscale OAuth migration step %s", payload.Step).LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "Tailscale OAuth migration step ran",
		attr.SlogAuditAction("tailscale_oauth_migration:"+payload.Step),
		attr.SlogAdminOIDCSubject(principal.ID),
	)

	// Read on the step's context: a closed tab must not turn a finished step
	// into an error.
	result, err := migration.status(stepCtx, organization.ID, organization.Name, organization.Slug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read Tailscale OAuth migration status").LogError(ctx, logger)
	}

	return result, nil
}

// status reads one organization's transition state and credential counts.
func (m *TailscaleOAuthMigration) status(ctx context.Context, organizationID, name, slug string) (*gen.AdminTailscaleOAuthMigrationOrganization, error) {
	transition, err := m.Transitions.GetFresh(ctx, organizationID, ingresscredentials.UseCase)
	if err != nil {
		return nil, fmt.Errorf("read transition state: %w", err)
	}

	counts, err := m.Store.Count(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("count credential copies: %w", err)
	}

	var stateChangedAt *string
	if !transition.StateChangedAt.IsZero() {
		stateChangedAt = rfc3339OrNil(&transition.StateChangedAt)
	}

	return &gen.AdminTailscaleOAuthMigrationOrganization{
		OrganizationID:          organizationID,
		Name:                    name,
		Slug:                    slug,
		State:                   string(transition.State),
		Generation:              transition.Generation,
		StateChangedAt:          stateChangedAt,
		Waiting:                 counts.Waiting,
		Unreadable:              counts.Unreadable,
		PlatformCopies:          counts.PlatformCopies,
		LastBackfillStartedAt:   rfc3339OrNil(transition.LastBackfill.StartedAt),
		LastBackfillCompletedAt: rfc3339OrNil(transition.LastBackfill.CompletedAt),
		LastBackfillError:       transition.LastBackfill.Error,
		LastVerifiedAt:          rfc3339OrNil(transition.LastVerifiedAt),
		PlatformCopiesClearedAt: rfc3339OrNil(transition.PlatformCopiesClearedAt),
	}, nil
}

// rfc3339OrNil renders an optional time as RFC 3339, or nil.
func rfc3339OrNil(t *time.Time) *string {
	if t == nil {
		return nil
	}

	return new(t.UTC().Format(time.RFC3339))
}
