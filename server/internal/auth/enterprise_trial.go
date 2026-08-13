package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// enterpriseTrialDuration bounds a self-signup enterprise trial. The trials
// table gives ends_at no default, so a change here leaves already-armed trials
// on the date they were given.
const enterpriseTrialDuration = 14 * 24 * time.Hour

// EnterpriseTrialBundleSeeder enables the entitlements an enterprise trial
// organization starts with. The dependency travels as a function because the
// productfeatures package that implements it imports auth.
type EnterpriseTrialBundleSeeder func(ctx context.Context, tx pgx.Tx, organizationID string) error

// armEnterpriseTrialTx turns an organization the caller's transaction just
// created into an enterprise trial. Every write joins that transaction, so a
// signup produces either a complete trial or no organization.
//
// A trial sits on the real enterprise tier rather than a tier of its own, so
// every account-type lookup downstream, including the OpenRouter credit
// ceiling, resolves against a value it already knows.
//
// actorEmail is the display name recorded on the audit entry. It travels as a
// parameter rather than an auth-context read because signup arms a trial from
// the unauthenticated callback, where there is no auth context to read.
func (s *Service) armEnterpriseTrialTx(ctx context.Context, tx pgx.Tx, org orgRepo.OrganizationMetadatum, userID, actorEmail string) error {
	if err := orgRepo.New(tx).SetAccountType(ctx, orgRepo.SetAccountTypeParams{
		ID:              org.ID,
		GramAccountType: string(billing.TierEnterprise),
	}); err != nil {
		return fmt.Errorf("set enterprise trial account type: %w", err)
	}

	if err := s.trialBundleSeeder(ctx, tx, org.ID); err != nil {
		return fmt.Errorf("seed enterprise trial entitlements: %w", err)
	}

	endsAt := time.Now().UTC().Add(enterpriseTrialDuration)
	if err := trialsRepo.New(tx).CreateTrial(ctx, trialsRepo.CreateTrialParams{
		OrganizationID: org.ID,
		Tier:           string(billing.TierEnterprise),
		EndsAt:         conv.ToPGTimestamptz(endsAt),
	}); err != nil {
		return fmt.Errorf("create enterprise trial: %w", err)
	}

	if err := s.auditLogger.LogOrganizationEnterpriseTrialArmed(ctx, tx, audit.LogOrganizationEnterpriseTrialArmedEvent{
		OrganizationID:   org.ID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		ActorDisplayName: conv.PtrEmpty(actorEmail),
		ActorSlug:        nil,
		OrganizationName: org.Name,
		OrganizationSlug: org.Slug,
		TrialEndsAt:      endsAt,
	}); err != nil {
		return fmt.Errorf("log enterprise trial armed: %w", err)
	}

	return nil
}
