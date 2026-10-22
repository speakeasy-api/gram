package organizations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// prepareOnboardingIssuer prepares sign-in before an administrator reaches the
// Anthropic connection step, including for existing organizations. It does not
// opt the organization into the gateway or migrate existing issuers or servers.
func (s *Service) prepareOnboardingIssuer(ctx context.Context, ac *contextvalues.AuthContext) error {
	// Read-only callers can view the checklist without acquiring mutation rights.
	if ac.APIKeyID != "" || contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin onboarding issuer preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	// Serialize repeated and concurrent visits before checking for an issuer.
	org, err := orgrepo.New(tx).LockOrganizationForSetupTaskUpdate(ctx, ac.ActiveOrganizationID)
	if err != nil {
		return fmt.Errorf("lock onboarding organization: %w", err)
	}
	if org.DisabledAt.Valid {
		return nil
	}
	_, reason, err := orggateway.Resolve(ctx, usersessionsrepo.New(tx), org.ID)
	if err != nil {
		return err
	}
	if reason != orggateway.UnavailabilityNoOrganizationIssuer {
		return nil
	}
	slug, err := conv.GenerateRandomSlug(8)
	if err != nil {
		return fmt.Errorf("generate onboarding issuer slug: %w", err)
	}
	// Match server-created user-session issuers: interactive organization sign-in,
	// fourteen-day sessions, and the create query's explicit admission default.
	row, err := usersessionsrepo.New(tx).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(org.ID),
		Slug:                         "organization-" + slug,
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: (14 * 24 * time.Hour).Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return fmt.Errorf("create onboarding issuer: %w", err)
	}
	if err := s.audit.LogUserSessionIssuerCreate(ctx, tx, audit.LogUserSessionIssuerCreateEvent{
		OrganizationID: org.ID, ProjectID: uuid.Nil,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), ActorDisplayName: ac.Email, ActorSlug: nil,
		UserSessionIssuerURN: urn.NewUserSessionIssuer(row.ID), Slug: row.Slug,
		UserSessionIssuerSnapshotAfter: usersessions.UserSessionIssuerView(row),
	}); err != nil {
		return fmt.Errorf("audit onboarding issuer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit onboarding issuer: %w", err)
	}
	return nil
}
