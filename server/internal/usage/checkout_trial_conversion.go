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
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

var errStripeCheckoutTrialLifecycleChanged = errors.New("trial lifecycle changed after Stripe Checkout preparation")

// convertEnterpriseTrialForCheckoutTx records the local conversion contract
// only. Provider reconciliation is deliberately left to the caller after the
// Stripe business transaction commits.
func (s *Service) convertEnterpriseTrialForCheckoutTx(ctx context.Context, tx pgx.Tx, organizationID string, expectedTrial *stripeCheckoutTrialFingerprint, preparedTrialFingerprint string, checkoutSessionID string) (bool, error) {
	trial, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		receiptAlreadyAttached, receiptErr := stripeCheckoutReceiptAttached(ctx, tx, organizationID, checkoutSessionID)
		if receiptErr != nil {
			return false, receiptErr
		}
		if receiptAlreadyAttached {
			return false, nil
		}
		if expectedTrial != nil || preparedTrialFingerprint != "none" {
			return false, errStripeCheckoutTrialLifecycleChanged
		}
		return false, nil
	case err != nil:
		return false, fmt.Errorf("lock enterprise trial checkout conversion: %w", err)
	}
	receiptAlreadyAttached, err := stripeCheckoutReceiptAttached(ctx, tx, organizationID, checkoutSessionID)
	if err != nil {
		return false, err
	}
	if receiptAlreadyAttached {
		return false, nil
	}
	currentTrial, err := trialsrepo.New(tx).GetTrial(ctx, organizationID)
	if err != nil {
		return false, fmt.Errorf("read locked trial lifecycle: %w", err)
	}
	if !stripeCheckoutTrialMatches(expectedTrial, currentTrial) || preparedTrialFingerprint != stripeCheckoutTrialFingerprintDigest(expectedTrial) {
		return false, errStripeCheckoutTrialLifecycleChanged
	}
	now := s.checkoutNow()
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
		Organization: audit.OrganizationEnterpriseTrialConversionOrganizationSnapshot{AccountType: "enterprise", Whitelisted: true, Disabled: organization.DisabledAt.Valid},
		Trial:        checkoutConversionTrialSnapshot(convertedTrial.Tier, convertedTrial.EndsAt, convertedTrial.ConvertedAt, convertedTrial.DemotedAt, now),
		Keys:         afterKeys,
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

func stripeCheckoutReceiptAttached(ctx context.Context, tx pgx.Tx, organizationID, checkoutSessionID string) (bool, error) {
	metadata, err := usagerepo.New(tx).GetBillingMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Stripe Checkout receipt under trial lock: %w", err)
	}
	return metadata.StripeCheckoutSessionID.Valid && metadata.StripeCheckoutSessionID.String == checkoutSessionID, nil
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
	status := "active"
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
