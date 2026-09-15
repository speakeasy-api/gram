package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/trials"
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
