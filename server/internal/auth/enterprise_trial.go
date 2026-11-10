package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// enterpriseTrialDays bounds a self-signup enterprise trial. The trials table
// gives ends_at no default, so a change here leaves already-armed trials on the
// date they were given.
const enterpriseTrialDays int32 = 14

// OrganizationFeatureSeeder enables baseline organization entitlements. The
// dependency travels as a function because the productfeatures package that
// implements it imports auth.
type OrganizationFeatureSeeder func(ctx context.Context, tx pgx.Tx, organizationID string) error

// EnterpriseTrialBundleSeeder enables the additional entitlements an enterprise
// trial organization starts with.
type EnterpriseTrialBundleSeeder = trials.BundleSeeder

// ArmEnterpriseTrialTx turns an organization into a self-signup enterprise
// trial with the standard runway and records the armed audit entry. Every
// write joins the caller's transaction. Callers must first establish that the
// organization has no existing trial lifecycle.
//
// actorEmail is the display name recorded on the audit entry. It travels as a
// parameter rather than an auth-context read because signup and invite callbacks
// have no auth context to read.
func ArmEnterpriseTrialTx(ctx context.Context, tx pgx.Tx, org orgRepo.OrganizationMetadatum, userID, actorEmail string, trialBundleSeeder EnterpriseTrialBundleSeeder, auditLogger *audit.Logger) error {
	armed, err := trials.ArmEnterpriseTrialTx(ctx, tx, trials.ArmParams{
		OrganizationID: org.ID,
		Days:           enterpriseTrialDays,
		Seeder:         trialBundleSeeder,
	})
	if err != nil {
		return fmt.Errorf("arm enterprise trial: %w", err)
	}

	if err := auditLogger.LogOrganizationEnterpriseTrialArmed(ctx, tx, audit.LogOrganizationEnterpriseTrialArmedEvent{
		OrganizationID:   org.ID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		ActorDisplayName: conv.PtrEmpty(actorEmail),
		ActorSlug:        nil,
		OrganizationName: armed.OrganizationName,
		OrganizationSlug: armed.OrganizationSlug,
		TrialEndsAt:      armed.EndsAt,
	}); err != nil {
		return fmt.Errorf("log enterprise trial armed: %w", err)
	}

	return nil
}

func (s *Service) armEnterpriseTrialTx(ctx context.Context, tx pgx.Tx, org orgRepo.OrganizationMetadatum, userID, actorEmail string) error {
	return ArmEnterpriseTrialTx(ctx, tx, org, userID, actorEmail, s.trialBundleSeeder, s.auditLogger)
}

// SetOrganizationFeatureSeederForTest replaces the baseline entitlement seeder.
// Tests use it to force self-serve trial arming to fail.
func (s *Service) SetOrganizationFeatureSeederForTest(seeder OrganizationFeatureSeeder) {
	s.organizationSeeder = seeder
}

// organizationEligibleForSelfServeTrial reports whether login may grant this
// organization the same enterprise trial a zero-organization signup receives.
// Prospects created in the admin app stay behind the book-a-demo gate, and so
// does any organization that is already on a paid tier or already inside the
// product.
func organizationEligibleForSelfServeTrial(org orgRepo.OrganizationMetadatum) bool {
	if org.ID == "" || org.Whitelisted || org.DisabledAt.Valid {
		return false
	}
	if billing.Tier(strings.ToLower(strings.TrimSpace(org.GramAccountType))) != billing.TierBase {
		return false
	}
	return conv.FromPGTextOrEmpty[string](org.CreationSource) != orgprovision.SourcePlatformAdmin
}

// armSelfServeTrialForExistingOrganization grants the standard signup trial to
// an organization that already existed when an organization admin signed in.
// Customers of another Speakeasy product already have an identity-provider
// organization, so the zero-organization signup path never runs for them and
// they would otherwise land on the book-a-demo gate. A membership without the
// admin role does not start the trial.
//
// armed is false when the organization is not eligible, the signer is not an
// admin, or a trial lifecycle already exists, including one that has ended. A
// concurrent login that commits first is also not armed here; that request
// sends the trial-started notification. The returned organization is the row
// after a successful arm, and the caller's row otherwise.
func (s *Service) armSelfServeTrialForExistingOrganization(ctx context.Context, org orgRepo.OrganizationMetadatum, userID, actorEmail string, membership identity.IDPMembershipRole) (orgRepo.OrganizationMetadatum, bool, error) {
	if !organizationEligibleForSelfServeTrial(org) || !slices.Contains(membership.RoleSlugs, authz.SystemRoleAdmin) {
		return org, false, nil
	}

	// Durable partner attribution excludes both pending and redeemed signups,
	// even when a later login carries no partner intent.
	partnerSignup, err := s.hasPartnerSignup(ctx, org.ID)
	if err != nil {
		return org, false, err
	}
	if partnerSignup {
		return org, false, nil
	}

	_, err = trialsRepo.New(s.db).GetTrial(ctx, org.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return org, false, fmt.Errorf("read trial lifecycle: %w", err)
	default:
		return org, false, nil
	}

	if s.organizationSeeder == nil || s.trialBundleSeeder == nil {
		return org, false, errors.New("enterprise trial seeders are not configured")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return org, false, fmt.Errorf("begin self-serve trial: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// The trial bundle grants Skills to the built-in member and admin roles.
	// An organization mirrored from the identity provider does not have those
	// roles until this seed runs.
	if err := authz.SeedSystemRoleGrantsTx(ctx, tx, org.ID); err != nil {
		return org, false, fmt.Errorf("provision organization access defaults: %w", err)
	}
	if err := assignSelfServeTrialMembershipRole(ctx, tx, org.ID, userID, membership); err != nil {
		return org, false, err
	}
	if err := s.organizationSeeder(ctx, tx, org.ID); err != nil {
		return org, false, fmt.Errorf("seed organization default entitlements: %w", err)
	}
	if err := s.armEnterpriseTrialTx(ctx, tx, org, userID, actorEmail); err != nil {
		if errors.Is(err, trials.ErrNotStartable) {
			return org, false, nil
		}
		return org, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return org, false, fmt.Errorf("commit self-serve trial: %w", err)
	}

	updated, err := s.orgRepo.GetOrganizationMetadata(ctx, org.ID)
	if err != nil {
		// The trial is already committed. Returning an error here would make
		// the caller skip the trial-started notification and claim the
		// transaction rolled back.
		s.logger.ErrorContext(ctx, "reload organization after self-serve trial",
			attr.SlogError(err),
			attr.SlogOrganizationID(org.ID),
		)
		return org, true, nil
	}
	return updated, true, nil
}

// membershipRoleForOrg returns the identity-provider membership for workosOrgID.
// The zero value means this organization had no membership in the login sync.
func membershipRoleForOrg(roles []identity.IDPMembershipRole, workosOrgID string) identity.IDPMembershipRole {
	var none identity.IDPMembershipRole
	if workosOrgID == "" {
		return none
	}
	for _, role := range roles {
		if role.WorkOSOrganizationID == workosOrgID {
			return role
		}
	}
	return none
}

// assignSelfServeTrialMembershipRole writes the signing-in user's identity-provider
// roles onto the organization that just received a trial. Built-in roles are
// seeded first, so admin and member slugs resolve. An empty slug list is left
// untouched: applying it would remove every assignment this user already holds.
// The write does not record a WorkOS event id, so a later membership webhook
// still applies.
func assignSelfServeTrialMembershipRole(ctx context.Context, tx pgx.Tx, organizationID, gramUserID string, membership identity.IDPMembershipRole) error {
	if membership.WorkOSUserID == "" || len(membership.RoleSlugs) == 0 {
		return nil
	}
	err := orgRepo.New(tx).SyncUserOrganizationRoleAssignments(ctx, orgRepo.SyncUserOrganizationRoleAssignmentsParams{
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		OrganizationID:     organizationID,
		WorkosUserID:       membership.WorkOSUserID,
		WorkosRoleSlugs:    membership.RoleSlugs,
		UserID:             conv.ToPGText(gramUserID),
		WorkosMembershipID: conv.ToPGTextEmpty(membership.WorkOSMembershipID),
	})
	if err != nil {
		return fmt.Errorf("assign self-serve trial membership role: %w", err)
	}
	return nil
}
