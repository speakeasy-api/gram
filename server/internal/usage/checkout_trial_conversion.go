package usage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// convertEnterpriseTrialForCheckoutTx records the local conversion contract
// only. Provider reconciliation is deliberately left to the caller after the
// Stripe business transaction commits.
func (s *Service) convertEnterpriseTrialForCheckoutTx(ctx context.Context, tx pgx.Tx, organizationID string, now time.Time) (bool, error) {
	trial, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("lock enterprise trial checkout conversion: %w", err)
	}
	if trial.ConvertedAt.Valid || trial.DemotedAt.Valid || trial.Tier != "enterprise" || !trial.EndsAt.Valid || !trial.EndsAt.Time.After(now) {
		return false, nil
	}
	provisioner, ok := s.openRouter.(checkoutTrialProvisioner)
	if !ok {
		return false, errors.New("model provider key lifecycle is unavailable")
	}

	for _, keyType := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, organizationID, keyType); err != nil {
			return false, fmt.Errorf("lock %s key for checkout conversion: %w", keyType, err)
		}
	}
	queries := adminrepo.New(tx)
	if _, err := queries.LockOrganizationMetadata(ctx, organizationID); err != nil {
		return false, fmt.Errorf("lock organization for checkout conversion: %w", err)
	}
	organization, err := queries.AdminGetOrganization(ctx, adminrepo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false})
	if err != nil {
		return false, fmt.Errorf("read organization for checkout conversion: %w", err)
	}
	demotedAccess := organization.AccountType == "free" && !organization.Whitelisted
	runningAccess := organization.AccountType == "enterprise" && organization.Whitelisted
	if !demotedAccess && !runningAccess {
		return false, errors.New("enterprise trial has incompatible organization access")
	}

	enterpriseFloor, ok := openrouter.DefaultCreditLimit(organizationID, billing.TierEnterprise, false)
	if !ok || enterpriseFloor <= 0 {
		return false, errors.New("enterprise tier has no OpenRouter credit policy")
	}
	keyChanges := make([]openrouter.EnterpriseTrialConversionKeyChange, 0, len(openrouter.AllKeyTypes))
	for _, keyType := range openrouter.AllKeyTypes {
		change, err := provisioner.PrepareEnterpriseTrialConversionKeyWithDB(ctx, tx, organizationID, keyType, int64(enterpriseFloor))
		if err != nil {
			return false, fmt.Errorf("prepare %s key for checkout conversion: %w", keyType, err)
		}
		if change.Exists {
			keyChanges = append(keyChanges, change)
		}
	}

	rows, err := trialsrepo.New(tx).MarkTrialConverted(ctx, organizationID)
	if err != nil || rows != 1 {
		return false, fmt.Errorf("mark enterprise trial converted: rows=%d: %w", rows, err)
	}
	if _, err := trialsrepo.New(tx).RestoreOrganizationFromTrial(ctx, trialsrepo.RestoreOrganizationFromTrialParams{OrganizationID: organizationID, AccountType: "enterprise"}); err != nil {
		return false, fmt.Errorf("restore organization after checkout conversion: %w", err)
	}
	if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, organizationID, true); err != nil {
		return false, fmt.Errorf("restore enterprise runtime features: %w", err)
	}
	convertedTrial, err := trialsrepo.New(tx).GetTrial(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("read converted enterprise trial: %w", err)
	}

	beforeKeys := make([]audit.OrganizationEnterpriseTrialConversionKeySnapshot, 0, len(keyChanges))
	afterKeys := make([]audit.OrganizationEnterpriseTrialConversionKeySnapshot, 0, len(keyChanges))
	for _, change := range keyChanges {
		beforeKeys = append(beforeKeys, checkoutConversionKeySnapshot(change.Before))
		afterKeys = append(afterKeys, checkoutConversionKeySnapshot(change.After))
	}
	before := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: organization.AccountType, Whitelisted: organization.Whitelisted, Disabled: organization.DisabledAt.Valid},
		Trial:        checkoutConversionTrialSnapshot(trial.Tier, trial.EndsAt, trial.ConvertedAt, trial.DemotedAt),
		Keys:         beforeKeys,
	}
	after := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: "enterprise", Whitelisted: true, Disabled: organization.DisabledAt.Valid},
		Trial:        checkoutConversionTrialSnapshot(convertedTrial.Tier, convertedTrial.EndsAt, convertedTrial.ConvertedAt, convertedTrial.DemotedAt),
		Keys:         afterKeys,
	}
	actorLabel := "System"
	if err := s.auditLogger.LogOrganizationEnterpriseTrialConverted(ctx, tx, audit.LogOrganizationEnterpriseTrialConvertedEvent{
		OrganizationID: organizationID, ConversionSource: "stripe_checkout",
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "system"), ActorDisplayName: &actorLabel, ActorSlug: nil,
		Before: before, After: after,
	}); err != nil {
		return false, fmt.Errorf("log enterprise trial checkout conversion: %w", err)
	}
	return true, nil
}

func checkoutConversionTrialSnapshot(tier string, endsAt, convertedAt, demotedAt pgtype.Timestamptz) audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot {
	return audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot{Tier: tier, EndsAt: checkoutPGTimePtr(endsAt), ConvertedAt: checkoutPGTimePtr(convertedAt), DemotedAt: checkoutPGTimePtr(demotedAt)}
}

func checkoutConversionKeySnapshot(state openrouter.EnterpriseTrialConversionKeyState) audit.OrganizationEnterpriseTrialConversionKeySnapshot {
	return audit.OrganizationEnterpriseTrialConversionKeySnapshot{
		KeyType: string(state.KeyType), DisableCauses: state.DisableCauses, StoredDisabled: state.Disabled,
		EffectiveDisabled: openrouter.EffectiveDisabled(state.Disabled, state.DisableCauses), MonthlyCredits: state.MonthlyCredits,
	}
}

func checkoutPGTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
