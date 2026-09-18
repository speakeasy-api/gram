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

var errStripeCheckoutTrialLifecycleChanged = errors.New("trial lifecycle changed after Stripe Checkout preparation")

// validateStripeCheckoutTrialTx keeps session creation tied to the prepared trial
// lifecycle without converting the trial or granting paid access.
func validateStripeCheckoutTrialTx(ctx context.Context, tx pgx.Tx, organizationID string, expectedTrial *stripeCheckoutTrialFingerprint, preparedTrialFingerprint string) error {
	_, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if expectedTrial != nil || preparedTrialFingerprint != "none" {
			return errStripeCheckoutTrialLifecycleChanged
		}
		return nil
	case err != nil:
		return fmt.Errorf("lock enterprise trial checkout lifecycle: %w", err)
	}
	currentTrial, err := trialsrepo.New(tx).GetTrial(ctx, organizationID)
	if err != nil {
		return fmt.Errorf("read locked trial lifecycle: %w", err)
	}
	if !stripeCheckoutTrialMatches(expectedTrial, currentTrial) || preparedTrialFingerprint != stripeCheckoutTrialFingerprintDigest(expectedTrial) {
		return errStripeCheckoutTrialLifecycleChanged
	}
	return nil
}

// convertEnterpriseTrialForCheckoutTx applies the active trial conversion only
// after Stripe confirms Checkout completion. The caller holds the trial, key,
// and organization locks; provider reconciliation follows the transaction commit.
func (s *Service) convertEnterpriseTrialForCheckoutTx(ctx context.Context, tx pgx.Tx, organizationID string, trial trialsrepo.Trial) (bool, error) {
	now := s.checkoutNow()
	if trial.ConvertedAt.Valid || trial.DemotedAt.Valid || trial.Tier != "enterprise" || !trial.EndsAt.Valid || !trial.EndsAt.Time.After(now) {
		return false, nil
	}
	provisioner, ok := s.openRouter.(checkoutTrialProvisioner)
	if !ok {
		return false, errors.New("model provider key lifecycle is unavailable")
	}

	queries := adminrepo.New(tx)
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
	// Self-serve Checkout is the PAYG conversion path. Admin conversion keeps enterprise.
	if _, err := trialsrepo.New(tx).RestoreOrganizationFromTrial(ctx, trialsrepo.RestoreOrganizationFromTrialParams{OrganizationID: organizationID, AccountType: string(billing.TierPayg)}); err != nil {
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
	keyAccessChanged := false
	for _, change := range keyChanges {
		accessChanged := openrouter.EffectiveDisabled(change.Before.Disabled, change.Before.DisableCauses) != openrouter.EffectiveDisabled(change.After.Disabled, change.After.DisableCauses)
		keyAccessChanged = keyAccessChanged || accessChanged
		beforeKeys = append(beforeKeys, checkoutConversionKeySnapshot(change.Before, accessChanged))
		afterKeys = append(afterKeys, checkoutConversionKeySnapshot(change.After, accessChanged))
	}
	before := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: organization.AccountType, Whitelisted: organization.Whitelisted, Disabled: organization.DisabledAt.Valid},
		Trial:        checkoutConversionTrialSnapshot(trial.Tier, trial.EndsAt, trial.ConvertedAt, trial.DemotedAt, now),
		Keys:         beforeKeys,
	}
	after := audit.OrganizationEnterpriseTrialConversionSnapshot{
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: string(billing.TierPayg), Whitelisted: true, Disabled: organization.DisabledAt.Valid},
		Trial:        checkoutConversionTrialSnapshot(convertedTrial.Tier, convertedTrial.EndsAt, convertedTrial.ConvertedAt, convertedTrial.DemotedAt, now),
		Keys:         afterKeys,
	}
	if s.auditLogger == nil {
		return false, errors.New("audit logger is unavailable")
	}
	actorLabel := "System"
	if err := s.auditLogger.LogOrganizationEnterpriseTrialConverted(ctx, tx, audit.LogOrganizationEnterpriseTrialConvertedEvent{
		OrganizationID: organizationID, ConversionSource: "stripe_checkout", KeyAccessChanged: &keyAccessChanged,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, "system"), ActorDisplayName: &actorLabel, ActorSlug: nil,
		Before: before, After: after,
	}); err != nil {
		return false, fmt.Errorf("log enterprise trial checkout conversion: %w", err)
	}
	return true, nil
}

func stripeCheckoutTrialMatches(expected *stripeCheckoutTrialFingerprint, actual trialsrepo.Trial) bool {
	if expected == nil {
		return false
	}
	return expected.organizationID == actual.OrganizationID &&
		expected.tier == actual.Tier &&
		expected.endsAt.Equal(actual.EndsAt.Time) &&
		checkoutOptionalTimesEqual(expected.convertedAt, checkoutOptionalTime(actual.ConvertedAt)) &&
		checkoutOptionalTimesEqual(expected.demotedAt, checkoutOptionalTime(actual.DemotedAt)) &&
		expected.createdAt.Equal(actual.CreatedAt.Time) &&
		expected.updatedAt.Equal(actual.UpdatedAt.Time)
}

func checkoutOptionalTimesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func checkoutConversionTrialSnapshot(tier string, endsAt, convertedAt, demotedAt pgtype.Timestamptz, now time.Time) audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot {
	status := "running"
	switch {
	case convertedAt.Valid:
		status = "converted"
	case demotedAt.Valid:
		status = "demoted"
	case !endsAt.Time.After(now):
		status = "expired"
	case !endsAt.Time.After(now.Add(7 * 24 * time.Hour)):
		status = "ending_soon"
	}
	return audit.OrganizationEnterpriseTrialConversionLifecycleSnapshot{Status: status, Tier: tier, EndsAt: checkoutPGTimePtr(endsAt), ConvertedAt: checkoutPGTimePtr(convertedAt), DemotedAt: checkoutPGTimePtr(demotedAt)}
}

func checkoutConversionKeySnapshot(state openrouter.EnterpriseTrialConversionKeyState, accessChanged bool) audit.OrganizationEnterpriseTrialConversionKeySnapshot {
	return audit.OrganizationEnterpriseTrialConversionKeySnapshot{
		KeyType: string(state.KeyType), StoredDisabled: state.Disabled,
		EffectiveDisabled: openrouter.EffectiveDisabled(state.Disabled, state.DisableCauses), KeyAccessChanged: accessChanged, MonthlyCredits: state.MonthlyCredits,
	}
}

func checkoutPGTimePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
