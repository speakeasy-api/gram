package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	usageRepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// Both admin entry points persist desired state and outbox intent together.
// Missing IDs retain bulk's established partial-match semantics; unsafe existing
// targets abort the whole transaction, including earlier targets in the batch.
func (s *Service) changeAccountTypes(ctx context.Context, ids []string, target string, whitelisted *bool) ([]string, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	conn, release, err := s.productFeatures.AcquireOrganizationFeatureCacheLocks(ctx, ids, productfeatures.TrialRuntimeFeatures)
	if err != nil {
		return nil, fmt.Errorf("acquire organization feature cache locks: %w", err)
	}
	defer release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin account type change: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	updated := []string{}
	for _, id := range ids {
		err = s.changeAccountTypeTx(ctx, tx, id, target, whitelisted)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		updated = append(updated, id)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit account type change: %w", err)
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var cacheErr error
	for _, id := range updated {
		for _, feature := range productfeatures.TrialRuntimeFeatures {
			cacheErr = errors.Join(cacheErr, s.productFeatures.UpdateFeatureCacheUnderLock(cacheCtx, conn, id, feature))
		}
		// Never notify for a rolled-back batch. Durable key reconciliation is driven
		// by the outbox event, not by an upstream write in this request.
		if err := s.trial.TrialInactive(ctx, id); err != nil {
			s.logger.WarnContext(ctx, "failed to stop trial notifications after tier change")
		}
	}
	if cacheErr != nil {
		s.logger.WarnContext(ctx, "tier change committed but feature cache refresh failed")
	}
	return updated, nil
}

func (s *Service) changeAccountTypeTx(ctx context.Context, tx pgx.Tx, id, target string, whitelisted *bool) error {
	trial, err := trialsRepo.New(tx).LockTrialLifecycle(ctx, id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock trial lifecycle: %w", err)
	}
	hasTrial := err == nil
	if hasTrial && trial.Tier == "enterprise" && target == string(billing.TierEnterprise) {
		return oops.E(oops.CodeConflict, nil, "enterprise trial conversion and retries require MarkEnterpriseTrialConverted")
	}
	for _, kt := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyBillingTransactionLock(ctx, tx, id, kt); err != nil {
			return fmt.Errorf("lock key billing: %w", err)
		}
	}
	for _, kt := range openrouter.AllKeyTypes {
		if err := openrouter.AcquireAPIKeyProvisioningTransactionLock(ctx, tx, id, kt); err != nil {
			return fmt.Errorf("lock key provisioning: %w", err)
		}
	}
	// Billing first writes hold this advisory lock before their FK check on
	// organization_metadata. Take it before the organization row to avoid a
	// lock inversion when billing metadata has not been created yet.
	bq := usageRepo.New(tx)
	if err := bq.LockBillingMetadataOrganization(ctx, id); err != nil {
		return fmt.Errorf("lock billing metadata organization: %w", err)
	}
	q := repo.New(tx)
	if _, err := q.LockOrganizationMetadata(ctx, id); err != nil {
		return fmt.Errorf("lock organization metadata: %w", err)
	}
	if _, err := bq.LockBillingMetadata(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock billing metadata: %w", err)
	}
	org, err := q.AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: id, AllowSlug: false})
	if err != nil {
		return fmt.Errorf("get organization: %w", err)
	}
	if target == string(billing.TierPayg) {
		if err := s.requireExistingBillingEligibility(ctx, org); err != nil {
			return err
		}
	}
	// An unconverted, undemoted trial continues to use the provider trial limit.
	activeTrial := hasTrial && !trial.ConvertedAt.Valid && !trial.DemotedAt.Valid
	credits, ok := openrouter.DefaultCreditLimit(id, billing.Tier(target), activeTrial)
	if !ok {
		return oops.E(oops.CodeConflict, nil, "target tier has no provider credit policy")
	}
	monthly := int64(credits)
	// These helpers only mutate caller-locked local rows; no provider client or
	// upstream credentials are needed to persist the desired policy.
	local := new(openrouter.OpenRouter)
	for _, kt := range openrouter.AllKeyTypes {
		policy := openrouter.AdminKeyPolicy{MonthlyCredits: &monthly, RemoveCauses: nil}
		if target == string(billing.TierPayg) || target == string(billing.TierEnterprise) {
			policy.RemoveCauses = []string{string(openrouter.DisableCauseTrialDemotion)}
		}
		if target == string(billing.TierPayg) && kt == openrouter.KeyTypeChat {
			policy.RemoveCauses = append(policy.RemoveCauses, string(openrouter.DisableCauseBillingInactive))
		}
		if _, err := local.PrepareAdminKeyPolicyWithDB(ctx, tx, id, kt, policy); err != nil {
			return fmt.Errorf("prepare account type key policy: %w", err)
		}
		if target == string(billing.TierBase) && kt == openrouter.KeyTypeChat && (!org.StripeSubscriptionID.Valid || org.StripeSubscriptionID.String == "") {
			if _, err := local.AddAPIKeyDisableCauseWithDB(ctx, tx, id, kt, openrouter.DisableCauseBillingInactive); err != nil {
				return fmt.Errorf("add inactive billing cause: %w", err)
			}
		}
	}
	if err := q.AdminUpdateOrganization(ctx, repo.AdminUpdateOrganizationParams{ID: id, AccountType: pgtype.Text{String: target, Valid: true}, Whitelisted: pgtype.Bool{Bool: false, Valid: false}}); err != nil {
		return fmt.Errorf("update organization account type: %w", err)
	}
	actor, display := enterpriseTrialConversionAuditActor(ctx)
	if whitelisted != nil {
		if _, err := SetOrganizationWhitelistTx(ctx, tx, s.audit, id, *whitelisted, actor, display); err != nil {
			return err
		}
	}
	// PAYG has verified billing. Enterprise without an enterprise trial follows the
	// contract tier policy. Pro has no equivalent evidence: retain runtime state.
	if target == string(billing.TierPayg) || target == string(billing.TierEnterprise) {
		if err := productfeatures.SetTrialRuntimeFeaturesTx(ctx, tx, id, true); err != nil {
			return fmt.Errorf("enable trial runtime features: %w", err)
		}
	}
	if err := s.audit.LogOrganizationAccountTypeChanged(ctx, tx, audit.LogOrganizationAccountTypeChangedEvent{OrganizationID: id, Actor: actor, ActorDisplayName: display, BeforeAccountType: org.AccountType, AccountType: target}); err != nil {
		return fmt.Errorf("audit account type change: %w", err)
	}
	return nil
}
